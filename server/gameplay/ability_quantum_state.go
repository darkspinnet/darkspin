package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const quantumStateLockout = 100 * time.Millisecond
const quantumStateSlowDuration = 3 * time.Second
const quantumStateStunDuration = time.Second
const quantumStateBanishDuration = time.Second
const quantumStateKnockbackDuration = 250 * time.Millisecond
const quantumStateKnockbackDistance = float32(3)
const quantumStateKnockbackSpeed = float32(12)

type quantumStateReactionKind uint8

const (
	quantumStateReactionSlow quantumStateReactionKind = iota
	quantumStateReactionStun
	quantumStateReactionKnockback
	quantumStateReactionArea
	quantumStateReactionBanish
)

type quantumStateRuntime struct {
	runtime        campaignDamageRuntime
	packet         raknet.Packet
	sessionKey     string
	generation     uint64
	ownerObjectID  uint32
	creatureIndex  uint32
	rank           int32
	activeAt       time.Time
	nextActivation time.Time
}

type quantumStateReactionStep struct {
	run            *heroModifierRun
	targetObjectID uint32
	timestamp      uint64
	kind           quantumStateReactionKind
}

type quantumStateStatusExpiry struct {
	npc        *zonenpc.Session
	pool       *modifierPool
	targetID   uint32
	instanceID uint32
	expiresAt  time.Time
	kind       quantumStateReactionKind
}

func (e quantumStateStatusExpiry) produce() ([][]byte, error) {
	if e.npc == nil || e.pool == nil || e.targetID == 0 || e.instanceID == 0 {
		return nil, nil
	}
	clearQuantumStateStatus(e.npc, e.targetID, e.expiresAt, e.kind)
	_ = e.pool.Release(e.instanceID)
	packet, err := effectraknet.ModifierDelete(e.targetID, e.instanceID)
	if err != nil {
		return nil, fmt.Errorf("quantumStateStatusDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func reserveQuantumStateReactionLocked(
	peerSession *gameplayPeerSession, sourceObjectID uint32,
	targetObjectID uint32, timestamp uint64,
) error {
	if peerSession == nil || peerSession.heroModifierRun == nil ||
		!peerSession.heroModifierRun.isQuantumState ||
		sourceObjectID != peerSession.deployedObjectID || targetObjectID == 0 ||
		peerSession.zone == nil || peerSession.zone.NPCs() == nil ||
		peerSession.zone.Population() == nil {
		return nil
	}
	run := peerSession.heroModifierRun
	now := run.quantum.runtime.npc.now()
	if now.Before(run.quantum.activeAt) || now.Before(run.quantum.nextActivation) {
		return nil
	}
	target, isFound := peerSession.zone.NPCs().NPC(targetObjectID)
	if !isFound || target.IsDefeated ||
		target.Faction != zonenpc.FactionNonPlayerAligned {
		return nil
	}
	choice, err := peerSession.zone.Population().Random().Index(5)
	if err != nil {
		return fmt.Errorf("quantumStateChoice: %w", err)
	}
	run.quantum.nextActivation = now.Add(quantumStateLockout)
	step := quantumStateReactionStep{
		run: run, targetObjectID: targetObjectID, timestamp: timestamp,
		kind: quantumStateReactionKind(choice),
	}
	_, err = run.quantum.packet.ScheduleProducers(
		[]raknet.ScheduledPacketProducer{{Produce: step.produce}},
	)
	if err != nil {
		run.quantum.nextActivation = time.Time{}
		return fmt.Errorf("quantumStateSchedule: %w", err)
	}
	return nil
}

func (r campaignDamageRuntime) reserveQuantumStateReaction(
	sessionKey string, generation uint64, sourceObjectID uint32,
	targetObjectID uint32, timestamp uint64,
) error {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation {
		r.registry.mutex.Unlock()
		return nil
	}
	err := reserveQuantumStateReactionLocked(
		&peerSession, sourceObjectID, targetObjectID, timestamp,
	)
	if err == nil {
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	return err
}

func (e quantumStateReactionStep) produce() ([][]byte, error) {
	runtime := e.run.quantum.runtime
	runtime.registry.mutex.Lock()
	peerSession, isFound := runtime.registry.sessions[e.run.quantum.sessionKey]
	isCurrent := isFound && peerSession.generation == e.run.quantum.generation &&
		peerSession.heroModifierRun == e.run && peerSession.zone != nil &&
		peerSession.zone.NPCs() != nil
	if !isCurrent {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if e.kind == quantumStateReactionArea {
		return e.produceAreaLocked(peerSession)
	}
	packets, expiry, err := e.produceStatusLocked(peerSession)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, err
	}
	if expiry.instanceID == 0 {
		runtime.registry.mutex.Unlock()
		return packets, nil
	}
	runtime.registry.sessions[e.run.quantum.sessionKey] = peerSession
	runtime.registry.mutex.Unlock()
	_, err = e.run.quantum.packet.ScheduleProducers(
		[]raknet.ScheduledPacketProducer{{
			Delay:   max(time.Duration(0), expiry.expiresAt.Sub(runtime.npc.now())),
			Produce: expiry.produce,
		}},
	)
	if err != nil {
		_, _ = expiry.produce()
		return nil, fmt.Errorf("quantumStateExpirySchedule: %w", err)
	}
	return packets, nil
}

func (e quantumStateReactionStep) produceStatusLocked(
	peerSession gameplayPeerSession,
) ([][]byte, quantumStateStatusExpiry, error) {
	runtime := e.run.quantum.runtime
	target, isFound := peerSession.zone.NPCs().NPC(e.targetObjectID)
	if !isFound || target.IsDefeated {
		return nil, quantumStateStatusExpiry{}, nil
	}
	duration := quantumStateSlowDuration
	modifierName := "QuantumSlowDebuff"
	if e.run.quantum.rank%2 == 0 {
		duration = time.Second
	}
	switch e.kind {
	case quantumStateReactionStun:
		duration = quantumStateStunDuration
		if e.run.quantum.rank%2 == 0 {
			duration = 500 * time.Millisecond
		}
		modifierName = "QuantumStunDebuff"
	case quantumStateReactionKnockback:
		duration = quantumStateKnockbackDuration
		modifierName = "QuantumKnockbackDebuff"
	case quantumStateReactionBanish:
		duration = quantumStateBanishDuration
		modifierName = "QuantumBanishDebuff"
	}
	expiresAt := runtime.npc.now().Add(duration)
	instanceID, err := runtime.npc.modifierPool.Allocate()
	if err != nil {
		return nil, quantumStateStatusExpiry{}, fmt.Errorf("quantumStateModifier: %w", err)
	}
	switch e.kind {
	case quantumStateReactionSlow:
		err = peerSession.zone.NPCs().ApplySlow(
			e.targetObjectID, expiresAt, 0.75, 1,
		)
	case quantumStateReactionStun:
		err = peerSession.zone.NPCs().ApplyStun(e.targetObjectID, expiresAt)
	case quantumStateReactionBanish:
		err = peerSession.zone.NPCs().ApplyBanish(e.targetObjectID, expiresAt)
	case quantumStateReactionKnockback:
		var movementPackets [][]byte
		movementPackets, err = e.applyKnockbackLocked(peerSession, target)
		if err == nil {
			packet, marshalErr := quantumStateModifierPacket(
				e.run.quantum.ownerObjectID, e.targetObjectID, modifierName,
				instanceID, duration, e.timestamp,
			)
			if marshalErr != nil {
				_ = runtime.npc.modifierPool.Release(instanceID)
				return nil, quantumStateStatusExpiry{}, marshalErr
			}
			movementPackets = append([][]byte{packet}, movementPackets...)
			return movementPackets, quantumStateStatusExpiry{
				npc: peerSession.zone.NPCs(), pool: runtime.npc.modifierPool,
				targetID: e.targetObjectID, instanceID: instanceID,
				expiresAt: expiresAt, kind: e.kind,
			}, nil
		}
	}
	if err != nil {
		_ = runtime.npc.modifierPool.Release(instanceID)
		return nil, quantumStateStatusExpiry{}, fmt.Errorf("quantumStateStatus: %w", err)
	}
	packet, err := quantumStateModifierPacket(
		e.run.quantum.ownerObjectID, e.targetObjectID, modifierName,
		instanceID, duration, e.timestamp,
	)
	if err != nil {
		clearQuantumStateStatus(
			peerSession.zone.NPCs(), e.targetObjectID, expiresAt, e.kind,
		)
		_ = runtime.npc.modifierPool.Release(instanceID)
		return nil, quantumStateStatusExpiry{}, err
	}
	return [][]byte{packet}, quantumStateStatusExpiry{
		npc: peerSession.zone.NPCs(), pool: runtime.npc.modifierPool,
		targetID: e.targetObjectID, instanceID: instanceID,
		expiresAt: expiresAt, kind: e.kind,
	}, nil
}

func clearQuantumStateStatus(
	npc *zonenpc.Session, targetObjectID uint32, expiresAt time.Time,
	kind quantumStateReactionKind,
) {
	if npc == nil {
		return
	}
	switch kind {
	case quantumStateReactionSlow:
		npc.ClearSlow(targetObjectID, expiresAt)
	case quantumStateReactionStun:
		npc.ClearStun(targetObjectID, expiresAt)
	case quantumStateReactionBanish:
		npc.ClearBanish(targetObjectID, expiresAt)
	}
}

func (e quantumStateReactionStep) applyKnockbackLocked(
	peerSession gameplayPeerSession, target zonenpc.Snapshot,
) ([][]byte, error) {
	if target.IsTurtleActive {
		return nil, nil
	}
	source := game.Vec3(peerSession.playerPosition)
	delta := target.Plan.Position.Sub(source)
	distance := delta.Length()
	if distance <= 0 {
		delta = game.Vec3{X: 1}
		distance = 1
	}
	desired := target.Plan.Position.Add(
		delta.Scale(quantumStateKnockbackDistance / distance),
	)
	destination, isFound, err := zoneaction.NPCDirectMovementDestination(
		peerSession.zone.Navigation(), target.Plan.Position, desired,
		max(target.Plan.NPCProfile.FootprintRadius, float32(0.25)),
	)
	if err != nil {
		return nil, fmt.Errorf("quantumStateKnockbackDestination: %w", err)
	}
	if !isFound {
		return nil, nil
	}
	plan := zonenpc.AttackPlan{
		SourceObjectID: e.run.quantum.ownerObjectID,
		TargetObjectID: e.targetObjectID,
		SourcePosition: source, TargetPosition: target.Plan.Position,
		Profile: zonenpc.ActionProfile{
			ForcedMovementSpeed:        quantumStateKnockbackSpeed,
			ForcedMovementDistance:     quantumStateKnockbackDistance,
			ForcedMovementReactionName: "react_knockback",
			ForcedMovementEffectName:   "QuantumKnockback",
		},
	}
	packets, err := npcraknet.ForcedMovement(plan, destination, e.timestamp)
	if err != nil {
		return nil, fmt.Errorf("quantumStateKnockbackMarshal: %w", err)
	}
	err = peerSession.zone.NPCs().SetPosition(e.targetObjectID, destination)
	if err != nil {
		return nil, fmt.Errorf("quantumStateKnockbackMove: %w", err)
	}
	return packets, nil
}

func (e quantumStateReactionStep) produceAreaLocked(
	peerSession gameplayPeerSession,
) ([][]byte, error) {
	runtime := e.run.quantum.runtime
	if e.run.quantum.creatureIndex >= uint32(len(peerSession.binding.Creatures)) ||
		peerSession.zone.Population() == nil {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	creature := peerSession.binding.Creatures[e.run.quantum.creatureIndex]
	definition := sim.AbilityDefinition{
		Name: "QuantumStateArea", Kind: sim.AbilityKindPointBlank,
		Radius: 3, IsAreaRadiusScaled: true,
		AnimationName: "sp_quantumRavager_support",
		MinimumDamage: 4, MaximumDamage: 8, DamageCoefficient: 0.06,
		DescriptorMask: 8, DamageType: 2, DamageSource: 1,
		IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
		ActivationEffectName: "spacetime_quantum_explosion_hit_effect.ServerEventDef",
		ImpactEffectName:     "spacetime_bite.ServerEventDef",
	}
	center := game.Vec3(peerSession.playerPosition)
	plan, err := zoneability.PlanArea(
		peerSession.zone.NPCs(), e.run.quantum.ownerObjectID,
		center, creature, definition,
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumStateAreaPlan: %w", err)
	}
	targets := plan.Target[:0]
	for _, target := range plan.Target {
		if target.Plan.ObjectID != e.targetObjectID {
			targets = append(targets, target)
		}
	}
	plan.Target = targets
	results, err := zoneability.CommitArea(
		peerSession.zone.Population().Random(), peerSession.zone.NPCs(), plan,
		creature, peerSession.binding.Difficulty, runtime.npc.program.Critical,
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumStateAreaDamage: %w", err)
	}
	transitions := make([]campaignDamageTransition, 0, len(results))
	for index, result := range results {
		transition, transitionErr := peerSession.applyCampaignDamageTransition(result.Damage)
		if transitionErr != nil {
			runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("quantumStateAreaTransition[%d]: %w", index, transitionErr)
		}
		transitions = append(transitions, transition)
	}
	runtime.registry.sessions[e.run.quantum.sessionKey] = peerSession
	runtime.registry.mutex.Unlock()
	effect := func(result zoneability.AreaResult) ([]byte, error) {
		packet, marshalErr := raknet.MarshalApplication(raknet.ServerEventMessage{
			Asset:    util.HashID(definition.ImpactEffectName),
			ObjectID: result.Snapshot.Plan.ObjectID,
		})
		if marshalErr != nil {
			return nil, fmt.Errorf("quantumStateAreaImpact: %w", marshalErr)
		}
		return packet, nil
	}
	presentation, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset:    util.HashID(definition.ActivationEffectName),
		ObjectID: e.run.quantum.ownerObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("quantumStateAreaEffect: %w", err)
	}
	packets, err := runtime.publishAreaResults(
		e.run.quantum.packet, e.run.quantum.sessionKey, e.run.quantum.generation,
		e.run.quantum.ownerObjectID, e.timestamp, peerSession.binding,
		results, transitions, effect, false,
	)
	if err != nil {
		return nil, fmt.Errorf("quantumStateAreaPublish: %w", err)
	}
	return append([][]byte{presentation}, packets...), nil
}

func quantumStateModifierPacket(
	sourceObjectID uint32, targetObjectID uint32, modifierName string,
	instanceID uint32, duration time.Duration, timestamp uint64,
) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: targetObjectID, ModifierGUID: util.HashID(modifierName),
		InstanceID: instanceID, DurationMilliseconds: uint32(duration.Milliseconds()),
		StackCount: 1, StartMilliseconds: timestamp, SourceID: sourceObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("quantumStateModifierMarshal: %w", err)
	}
	return packet, nil
}
