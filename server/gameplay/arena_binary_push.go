package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// Kinetic Wave's force mover is target-owned. Its attractor is an attached
// effect, not a modifier, and retires after the authored movement outro.
func (e *arenaAbilityTargets) applyBinaryPushesLocked(plan zoneability.AreaPlan,
	results []zoneability.AreaResult,
) ([][]byte, error) {
	packets := make([][]byte, 0)
	match := e.runtime.registry.arenaMatches[e.source.binding.GameID]
	if match == nil || !match.isCombatActive || match.navigation == nil || e.runtime.registry.timer == nil {
		return nil, nil
	}
	for _, result := range results {
		if result.Damage.IsDamageImmune || result.Damage.IsDefeated || result.Damage.IsTurtleStarted || result.Damage.Damage <= 0 {
			continue
		}
		key, victim, isFound := e.member(result.Damage.ObjectID)
		if !isFound || victim.isEnemyRootActive(e.runtime.now()) {
			continue
		}
		sourcePosition := game.Vec3(e.source.playerPosition)
		targetPosition := game.Vec3(victim.playerPosition)
		delta := targetPosition.Sub(sourcePosition)
		distance := delta.Length()
		if distance <= 0 {
			delta = game.Vec3{X: 1}
			distance = 1
		}
		desired := targetPosition.Add(delta.Scale(binarySentinelPushDistance / distance))
		destination, isReachable, err := navigationClippedMovementDestination(match.navigation,
			targetPosition, desired, max(victim.deployedCampaignNavigationRadius(), float32(.25)))
		if err != nil {
			return packets, fmt.Errorf("arenaBinaryDestination: %w", err)
		}
		if !isReachable {
			continue
		}
		jump := zonenpc.AttackPlan{SourceObjectID: plan.SourceObjectID,
			TargetObjectID: victim.deployedObjectID, SourcePosition: sourcePosition,
			TargetPosition: targetPosition,
			Profile: zonenpc.ActionProfile{ForcedMovementSpeed: binarySentinelPushSpeed,
				ForcedMovementDistance: binarySentinelPushDistance, ForcedMovementReactionName: "react_knockback"}}
		jumpPackets, err := npcraknet.ForcedJump(jump, destination, e.timestamp, [3]float32{2, 2, 4})
		if err != nil {
			return packets, fmt.Errorf("arenaBinaryJump: %w", err)
		}
		effects, err := e.runtime.registry.allocateAbilityChannelEffectsLocked(victim,
			e.runtime.effectPool, []uint32{victim.deployedObjectID})
		if err != nil {
			return packets, fmt.Errorf("arenaBinaryEffectSlot: %w", err)
		}
		slot := effects.Slots()[0].slot
		effectPacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: slot + 1, ObjectID: victim.deployedObjectID,
			Asset: util.HashID(binarySentinelPushEffectName), IsForceAttached: true})
		if err != nil {
			effects.Retire()
			e.runtime.registry.commitChannelRemovalsLocked()
			return packets, fmt.Errorf("arenaBinaryEffect: %w", err)
		}
		now := e.runtime.now()
		travel := basicKnockbackTravel(jump, destination)
		run := &arenaHitPushRun{runtime: e.runtime, targetKey: key, gameID: victim.binding.GameID,
			generation: victim.generation, transportGeneration: victim.transportGeneration,
			squad: victim.squad, noun: victim.binding.Creatures[victim.deployedCreatureIndex].Noun,
			index: victim.deployedCreatureIndex, objectID: victim.deployedObjectID,
			modifierID: util.HashID(binarySentinelSupportName), effects: effects,
			landingAt: now.Add(travel), holdUntil: now.Add(travel + binarySentinelPushOutro),
			landingTime: e.timestamp + uint64(travel/time.Millisecond)}
		run.landingCancel, err = e.runtime.registry.timer.Schedule(max(time.Millisecond, travel), run.land)
		if err == nil && run.landingCancel == nil {
			err = errors.New("push landing cancellation unavailable")
		}
		if err != nil {
			run.release(nil)
			return packets, fmt.Errorf("arenaBinaryLanding: %w", err)
		}
		run.expiryCancel, err = e.runtime.registry.timer.Schedule(travel+binarySentinelPushOutro, run.expire)
		if err == nil && run.expiryCancel == nil {
			err = errors.New("push outro cancellation unavailable")
		}
		if err != nil {
			run.release(nil)
			return packets, fmt.Errorf("arenaBinaryOutro: %w", err)
		}
		err = victim.placeTeleportedPlayer(now, raknet.Vector3(destination))
		if err != nil {
			run.release(nil)
			return packets, fmt.Errorf("arenaBinaryPlacement: %w", err)
		}
		if previous := victim.arenaHitPushes[run.modifierID]; previous != nil {
			packets = append(packets, previous.release(&victim)...)
		}
		if interrupted := victim.resetInterruptibleActionAdmission(); interrupted != nil {
			interrupted.Stop()
		}
		victim.campaignPlayerPursuitSession().Cancel()
		victim.heroKnockbackLanding = heroKnockbackLanding{objectID: run.objectID,
			readyAt: run.landingAt, holdUntil: run.holdUntil, isNativeJump: true}
		run.motionRevision = victim.playerMotionRevision()
		if victim.arenaHitPushes == nil {
			victim.arenaHitPushes = make(map[uint32]*arenaHitPushRun)
		}
		victim.arenaHitPushes[run.modifierID] = run
		e.runtime.registry.sessions[key] = victim
		err = e.runtime.registry.publishChannelEffectsLocked(effects, [][]byte{effectPacket})
		if err != nil {
			packets = append(packets, run.release(&victim)...)
			e.runtime.registry.sessions[key] = victim
			return packets, fmt.Errorf("arenaBinaryPublish: %w", err)
		}
		packets = append(packets, jumpPackets...)
	}
	return packets, nil
}
