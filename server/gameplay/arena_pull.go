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

// Binary Sentinel's pull uses the same recipient-owned forced-movement life
// cycle as Comet. The target lands at the caster's edge and releases after the
// authored 250ms outro; an obstructed path clips to the reachable point.
func (e *arenaAbilityTargets) applyBinaryPullsLocked(plan zoneability.AreaPlan,
	results []zoneability.AreaResult,
) ([][]byte, error) {
	packets := make([][]byte, 0)
	match := e.runtime.registry.arenaMatches[e.source.binding.GameID]
	if match == nil || !match.isCombatActive || match.navigation == nil || e.runtime.registry.timer == nil {
		return nil, nil
	}
	for _, result := range results {
		if result.Damage.IsDamageImmune || result.Damage.IsDefeated || result.Damage.IsTurtleStarted {
			continue
		}
		key, victim, isFound := e.member(result.Damage.ObjectID)
		if !isFound || victim.isEnemyRootActive(e.runtime.now()) {
			continue
		}
		sourcePosition := game.Vec3(e.source.playerPosition)
		targetPosition := game.Vec3(victim.playerPosition)
		delta := sourcePosition.Sub(targetPosition)
		distance := delta.Length()
		edgeDistance := distance - e.source.deployedCampaignFootprintRadius() - victim.deployedCampaignFootprintRadius()
		if distance <= 0 || edgeDistance <= 0 {
			continue
		}
		desired := targetPosition.Add(delta.Scale(edgeDistance / distance))
		destination, isReachable, err := navigationClippedMovementDestination(match.navigation,
			targetPosition, desired, max(victim.deployedCampaignNavigationRadius(), float32(.25)))
		if err != nil {
			return packets, fmt.Errorf("arenaPullDestination: %w", err)
		}
		if !isReachable {
			continue
		}
		movementDistance := destination.Sub(targetPosition).Length()
		jump := zonenpc.AttackPlan{SourceObjectID: plan.SourceObjectID,
			TargetObjectID: victim.deployedObjectID, SourcePosition: sourcePosition,
			TargetPosition: targetPosition,
			Profile: zonenpc.ActionProfile{ForcedMovementSpeed: binarySentinelPullSpeed,
				ForcedMovementDistance: movementDistance, ForcedMovementReactionName: "react_pulled"}}
		jumpPackets, err := npcraknet.ForcedJump(jump, destination, e.timestamp, [3]float32{})
		if err != nil {
			return packets, fmt.Errorf("arenaPullJump: %w", err)
		}
		instanceID, err := e.runtime.modifierPool.Allocate()
		if err != nil {
			return packets, fmt.Errorf("arenaPullAllocate: %w", err)
		}
		now := e.runtime.now()
		travel := basicKnockbackTravel(jump, destination)
		run := &arenaHitPushRun{runtime: e.runtime, targetKey: key, gameID: victim.binding.GameID,
			generation: victim.generation, transportGeneration: victim.transportGeneration,
			squad: victim.squad, noun: victim.binding.Creatures[victim.deployedCreatureIndex].Noun,
			index: victim.deployedCreatureIndex, objectID: victim.deployedObjectID,
			modifierID: util.HashID("BinarySentinelPullModifier"), instanceID: instanceID,
			landingAt: now.Add(travel), holdUntil: now.Add(travel + binarySentinelPullOutro),
			landingTime: e.timestamp + uint64(travel/time.Millisecond)}
		created, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
			TargetID: run.objectID, ModifierGUID: run.modifierID, InstanceID: instanceID,
			StackCount: 1, DurationMilliseconds: uint32((travel + binarySentinelPullOutro).Milliseconds()),
			StartMilliseconds: e.timestamp, NPCAffixAssetID: 0})
		if err != nil {
			return packets, run.discard("arenaPullCreate", err)
		}
		run.landingCancel, err = e.runtime.registry.timer.Schedule(max(time.Millisecond, travel), run.land)
		if err == nil && run.landingCancel == nil {
			err = errors.New("pull landing cancellation unavailable")
		}
		if err != nil {
			return packets, run.discard("arenaPullLanding", err)
		}
		run.expiryCancel, err = e.runtime.registry.timer.Schedule(travel+binarySentinelPullOutro, run.expire)
		if err == nil && run.expiryCancel == nil {
			err = errors.New("pull outro cancellation unavailable")
		}
		if err != nil {
			return packets, run.discard("arenaPullOutro", err)
		}
		err = victim.placeTeleportedPlayer(now, raknet.Vector3(destination))
		if err != nil {
			return packets, run.discard("arenaPullPlace", err)
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
		packets = append(packets, created)
		packets = append(packets, jumpPackets...)
	}
	return packets, nil
}
