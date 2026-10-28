package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	"github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type arenaQuantumStateReaction struct {
	runtime       campaignAbilityCommandRuntime
	ownerKey      string
	ownerRun      *heroModifierRun
	ownerObjectID uint32
	attackerID    uint32
	gameID        uint32
	timestamp     uint64
	kind          quantumStateReactionKind
}

// Called after an accepted Arena hit while the registry is locked. The zero
// delay callback keeps the reactive hit outside the damage commit stack.
func (e *arenaAbilityTargets) reserveArenaQuantumStateLocked(key string, victim *gameplayPeerSession,
	attackerID uint32,
) error {
	run := victim.heroModifierRun
	if run == nil || !run.isQuantumState || victim.deployedHitPoint() <= 0 ||
		victim.deployedObjectID != run.objectID || e.runtime.registry.timer == nil {
		return nil
	}
	match := e.runtime.registry.arenaMatches[victim.binding.GameID]
	if match == nil || !match.isCombatActive {
		return nil
	}
	now := e.runtime.now()
	if now.Before(run.quantum.activeAt) || now.Before(run.quantum.nextActivation) {
		return nil
	}
	query := arenaAbilityTargets{runtime: e.runtime, source: *victim}
	attackerKey, attacker, isFound := query.member(attackerID)
	if !isFound || attackerKey == "" || attacker.isArenaBanished(now) {
		return nil
	}
	choice, err := match.random.Index(5)
	if err != nil {
		return fmt.Errorf("arenaQuantumChoice: %w", err)
	}
	run.quantum.nextActivation = now.Add(quantumStateLockout)
	if e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena Quantum State reaction selected game=%d owner=%d attacker=%d kind=%d",
			victim.binding.GameID, victim.deployedObjectID, attackerID, choice)
	}
	reaction := arenaQuantumStateReaction{runtime: e.runtime, ownerKey: key, ownerRun: run,
		ownerObjectID: victim.deployedObjectID, attackerID: attackerID,
		gameID: victim.binding.GameID, timestamp: e.timestamp, kind: quantumStateReactionKind(choice)}
	cancel, err := e.runtime.registry.timer.Schedule(0, reaction.apply)
	if err == nil && cancel == nil {
		err = errors.New("reaction cancellation unavailable")
	}
	if err != nil {
		run.quantum.nextActivation = time.Time{}
		return fmt.Errorf("arenaQuantumSchedule: %w", err)
	}
	return nil
}

func (e arenaQuantumStateReaction) apply() {
	e.runtime.registry.mutex.Lock()
	owner, isFound := e.runtime.registry.sessions[e.ownerKey]
	match := e.runtime.registry.arenaMatches[e.gameID]
	if !isFound || match == nil || !match.isCombatActive || owner.binding.Mode != game.ModeArena ||
		owner.binding.GameID != e.gameID || owner.deployedObjectID != e.ownerObjectID ||
		owner.deployedHitPoint() <= 0 || owner.heroModifierRun != e.ownerRun {
		e.runtime.registry.mutex.Unlock()
		return
	}
	query := arenaAbilityTargets{runtime: e.runtime, source: owner}
	targetSnapshot, isTargetPresent := query.NPC(e.attackerID)
	if !isTargetPresent || targetSnapshot.Plan.ObjectID != e.attackerID {
		e.runtime.registry.mutex.Unlock()
		return
	}
	targetKey, target, isTargetFound := query.member(e.attackerID)
	if !isTargetFound || targetKey == "" {
		e.runtime.registry.mutex.Unlock()
		return
	}
	var err error
	var targets *arenaAbilityTargets
	switch e.kind {
	case quantumStateReactionArea:
		targets, err = e.areaLocked(owner, match)
	case quantumStateReactionKnockback:
		err = e.knockbackLocked(owner, target, match)
	default:
		err = e.statusLocked(owner)
	}
	e.runtime.registry.mutex.Unlock()
	if targets != nil {
		targets.finish()
	}
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena Quantum State reaction failed owner=%d target=%d kind=%d: %v",
			e.ownerObjectID, e.attackerID, e.kind, err)
	}
}

func (e arenaQuantumStateReaction) statusLocked(owner gameplayPeerSession) error {
	duration := quantumStateSlowDuration
	kind := sim.AbilityStatusKindSlow
	modifierName := "QuantumSlowDebuff"
	if e.ownerRun.quantum.rank%2 == 0 {
		duration = time.Second
	}
	switch e.kind {
	case quantumStateReactionStun:
		kind = sim.AbilityStatusKindStun
		modifierName = "QuantumStunDebuff"
		duration = quantumStateStunDuration
		if e.ownerRun.quantum.rank%2 == 0 {
			duration = 500 * time.Millisecond
		}
	case quantumStateReactionBanish:
		kind = sim.AbilityStatusKindBanish
		modifierName = "QuantumBanishDebuff"
		duration = quantumStateBanishDuration
	}
	definition := sim.AbilityDefinition{Name: "QuantumStateReaction", StatusKind: kind,
		StatusDuration: duration, RootModifierID: util.HashID(modifierName),
		StatusMovementScale: .75, StatusAttackScale: 1}
	plan := zoneability.AreaPlan{SourceObjectID: e.ownerObjectID, Definition: definition}
	packets, err := e.runtime.applyArenaTargetStatusLocked(e.ownerKey, owner.generation,
		e.ownerObjectID, e.timestamp, plan, []uint32{e.attackerID})
	if err != nil {
		return fmt.Errorf("arenaQuantumStatus: %w", err)
	}
	if len(packets) != 0 {
		e.runtime.registry.queueFieldMedicRealmPacketsLocked(nil, e.gameID, packets)
	}
	return nil
}

func (e arenaQuantumStateReaction) areaLocked(owner gameplayPeerSession,
	match *arenaMatchState,
) (*arenaAbilityTargets, error) {
	if e.ownerRun.quantum.creatureIndex >= uint32(len(owner.binding.Creatures)) {
		return nil, nil
	}
	creature := owner.binding.Creatures[e.ownerRun.quantum.creatureIndex]
	definition := sim.AbilityDefinition{Name: "QuantumStateArea", Kind: sim.AbilityKindPointBlank,
		Radius: 3, IsAreaRadiusScaled: true, MinimumDamage: 4, MaximumDamage: 8,
		DamageCoefficient: .06, DescriptorMask: 8, DamageType: 2, DamageSource: 1,
		IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
		ActivationEffectName: "spacetime_quantum_explosion_hit_effect.ServerEventDef",
		ImpactEffectName:     "spacetime_bite.ServerEventDef"}
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, owner, definition, e.timestamp).(*arenaAbilityTargets)
	plan, err := zoneability.PlanArea(targets, e.ownerObjectID,
		game.Vec3(owner.playerPosition), creature, definition)
	if err != nil {
		return nil, fmt.Errorf("arenaQuantumAreaPlan: %w", err)
	}
	selected := plan.Target[:0]
	for _, candidate := range plan.Target {
		if candidate.Plan.ObjectID != e.attackerID {
			selected = append(selected, candidate)
		}
	}
	plan.Target = selected
	results, err := zoneability.CommitArea(match.random, targets, plan, creature,
		owner.binding.Difficulty, e.runtime.program.Critical)
	if err != nil {
		return targets, fmt.Errorf("arenaQuantumAreaHit: %w", err)
	}
	effect := func(result zoneability.AreaResult) ([]byte, error) {
		packet, marshalErr := raknet.MarshalApplication(raknet.ServerEventMessage{
			Asset: util.HashID(definition.ImpactEffectName), ObjectID: result.Snapshot.Plan.ObjectID})
		if marshalErr != nil {
			return nil, fmt.Errorf("arenaQuantumAreaImpact: %w", marshalErr)
		}
		return packet, nil
	}
	packets, err := targets.packetsWithEffects(results, effect, false)
	if err != nil {
		return targets, fmt.Errorf("arenaQuantumAreaPackets: %w", err)
	}
	presentation, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: util.HashID(definition.ActivationEffectName), ObjectID: e.ownerObjectID})
	if err != nil {
		return targets, fmt.Errorf("arenaQuantumAreaEffect: %w", err)
	}
	e.runtime.registry.queueFieldMedicRealmPacketsLocked(nil, e.gameID,
		append([][]byte{presentation}, packets...))
	return targets, nil
}

func (e arenaQuantumStateReaction) knockbackLocked(owner, target gameplayPeerSession,
	match *arenaMatchState,
) error {
	if match.navigation == nil || e.runtime.registry.timer == nil || target.isEnemyRootActive(e.runtime.now()) {
		return nil
	}
	sourcePosition := game.Vec3(owner.playerPosition)
	targetPosition := game.Vec3(target.playerPosition)
	delta := targetPosition.Sub(sourcePosition)
	distance := delta.Length()
	if distance <= 0 {
		delta = game.Vec3{X: 1}
		distance = 1
	}
	desired := targetPosition.Add(delta.Scale(quantumStateKnockbackDistance / distance))
	destination, isReachable, err := navigationClippedMovementDestination(match.navigation,
		targetPosition, desired, max(target.deployedCampaignNavigationRadius(), float32(.25)))
	if err != nil {
		return fmt.Errorf("arenaQuantumDestination: %w", err)
	}
	if !isReachable {
		return nil
	}
	jump := npc.AttackPlan{SourceObjectID: e.ownerObjectID, TargetObjectID: e.attackerID,
		SourcePosition: sourcePosition, TargetPosition: targetPosition,
		Profile: npc.ActionProfile{ForcedMovementSpeed: quantumStateKnockbackSpeed,
			ForcedMovementDistance:     quantumStateKnockbackDistance,
			ForcedMovementReactionName: "react_knockback", ForcedMovementEffectName: "QuantumKnockback"}}
	jumpPackets, err := npcraknet.ForcedJump(jump, destination, e.timestamp, [3]float32{2, 4, 4})
	if err != nil {
		return fmt.Errorf("arenaQuantumJump: %w", err)
	}
	instanceID, err := e.runtime.modifierPool.Allocate()
	if err != nil {
		return fmt.Errorf("arenaQuantumAllocate: %w", err)
	}
	key, current, isFound := (arenaAbilityTargets{runtime: e.runtime, source: owner}).member(e.attackerID)
	if !isFound {
		releaseErr := e.runtime.modifierPool.Release(instanceID)
		if releaseErr != nil {
			return fmt.Errorf("arenaQuantumTargetRelease: %w", releaseErr)
		}
		return nil
	}
	now := e.runtime.now()
	travel := basicKnockbackTravel(jump, destination)
	run := &arenaHitPushRun{runtime: e.runtime, targetKey: key, gameID: e.gameID,
		generation: current.generation, transportGeneration: current.transportGeneration,
		squad: current.squad, noun: current.binding.Creatures[current.deployedCreatureIndex].Noun,
		index: current.deployedCreatureIndex, objectID: current.deployedObjectID,
		modifierID: util.HashID("QuantumKnockbackDebuff"), instanceID: instanceID,
		landingAt: now.Add(travel), holdUntil: now.Add(travel + quantumStateKnockbackDuration),
		landingTime: e.timestamp + uint64(travel/time.Millisecond)}
	created, err := quantumStateModifierPacket(e.ownerObjectID, e.attackerID,
		"QuantumKnockbackDebuff", instanceID, quantumStateKnockbackDuration, e.timestamp)
	if err != nil {
		return run.discard("arenaQuantumCreate", err)
	}
	run.landingCancel, err = e.runtime.registry.timer.Schedule(max(time.Millisecond, travel), run.land)
	if err == nil && run.landingCancel == nil {
		err = errors.New("landing cancellation unavailable")
	}
	if err != nil {
		return run.discard("arenaQuantumLanding", err)
	}
	run.expiryCancel, err = e.runtime.registry.timer.Schedule(travel+quantumStateKnockbackDuration, run.expire)
	if err == nil && run.expiryCancel == nil {
		err = errors.New("outro cancellation unavailable")
	}
	if err != nil {
		return run.discard("arenaQuantumOutro", err)
	}
	err = current.placeTeleportedPlayer(now, raknet.Vector3(destination))
	if err != nil {
		return run.discard("arenaQuantumPlacement", err)
	}
	if current.arenaHitPushes == nil {
		current.arenaHitPushes = make(map[uint32]*arenaHitPushRun)
	}
	if previous := current.arenaHitPushes[run.modifierID]; previous != nil {
		e.runtime.registry.queueFieldMedicRealmPacketsLocked(nil, e.gameID, previous.release(&current))
	}
	if interrupted := current.resetInterruptibleActionAdmission(); interrupted != nil {
		interrupted.Stop()
	}
	current.campaignPlayerPursuitSession().Cancel()
	current.heroKnockbackLanding = heroKnockbackLanding{objectID: run.objectID,
		readyAt: run.landingAt, holdUntil: run.holdUntil, isNativeJump: true}
	run.motionRevision = current.playerMotionRevision()
	current.arenaHitPushes[run.modifierID] = run
	e.runtime.registry.sessions[key] = current
	e.runtime.registry.queueFieldMedicRealmPacketsLocked(nil, e.gameID,
		append([][]byte{created}, jumpPackets...))
	return nil
}
