package gameplay

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const (
	fireTempestChargeDuration = 8 * time.Second
	fireTempestTargetPoll     = 250 * time.Millisecond
	fireTempestHitDelay       = 100 * time.Millisecond
	fireTempestRearmDuration  = 2 * time.Second
	fireTempestStunDuration   = 2 * time.Second
)

var fireTempestPassiveDefinition = sim.AbilityDefinition{
	Name: "FireTempestPassive", Kind: sim.AbilityKindAuraArea,
	MinimumDamage: 4, MaximumDamage: 6, DamageCoefficient: 0.05, Radius: 5,
	DescriptorMask: 32, DamageType: 3, DamageSource: 0,
	IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
	ActivationEffectName: "fire_rocks.ServerEventDef",
	ImpactEffectName:     "fire_rocks_impact.ServerEventDef",
}

type fireTempestPassivePhase uint8

const (
	fireTempestCharging fireTempestPassivePhase = iota
	fireTempestWaitingTarget
	fireTempestHitPending
	fireTempestRearming
	fireTempestStopped
)

type fireTempestPassiveRun struct {
	mutex      sync.Mutex
	cancel     raknet.CancelSchedule
	effectPool *attachedEffectPool
	objectID   uint32
	effectSlot uint8
	phase      fireTempestPassivePhase
}

func (e *fireTempestPassiveRun) Stop() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	e.phase = fireTempestStopped
	cancel := e.cancel
	e.cancel = nil
	e.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
	if e.effectPool != nil {
		e.effectPool.Release(e.objectID, e.effectSlot)
	}
}

func (e *fireTempestPassiveRun) setCancel(cancel raknet.CancelSchedule) bool {
	if e == nil {
		if cancel != nil {
			cancel()
		}
		return false
	}
	e.mutex.Lock()
	if e.phase == fireTempestStopped {
		e.mutex.Unlock()
		if cancel != nil {
			cancel()
		}
		return false
	}
	e.cancel = cancel
	e.mutex.Unlock()
	return true
}

func (e *fireTempestPassiveRun) transition(
	from fireTempestPassivePhase, to fireTempestPassivePhase,
) bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.phase != from {
		return false
	}
	e.phase = to
	return true
}

func (e *fireTempestPassiveRun) transitionWaiting(
	to fireTempestPassivePhase,
) bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.phase != fireTempestCharging && e.phase != fireTempestWaitingTarget {
		return false
	}
	e.phase = to
	return true
}

type fireTempestPassiveRuntime struct {
	registry   *gameplaySessionRegistry
	damage     campaignDamageRuntime
	effectPool *attachedEffectPool
	program    Programs
	now        func() time.Time
}

type fireTempestPassiveSchedule struct {
	runtime    fireTempestPassiveRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	creatureID uint32
	sourceTime uint64
	startedAt  time.Time
	run        *fireTempestPassiveRun
}

func (e fireTempestPassiveSchedule) schedule(
	delay time.Duration, produce func() ([][]byte, error),
) error {
	producer := raknet.ScheduledPacketProducer{Delay: delay, Produce: produce}
	cancel, err := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{producer})
	if err != nil {
		return fmt.Errorf("firePassiveSchedule: %w", err)
	}
	e.run.setCancel(cancel)
	return nil
}

func (e fireTempestPassiveSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.deployedObjectID == e.objectID &&
		peerSession.deployedCreatureIndex == e.creatureID &&
		peerSession.fireTempestPassive == e.run
}

func (e fireTempestPassiveSchedule) chargeReady() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound) && peerSession.zone != nil &&
		peerSession.zone.NPCs() != nil
	isTargetFound := false
	if isCurrent {
		center := game.Vec3(peerSession.playerPosition)
		for _, npc := range peerSession.zone.NPCs().LiveSnapshots() {
			if npc.Faction != zonenpc.FactionNonPlayerAligned ||
				npc.Plan.Position.Sub(center).Length() >
					fireTempestPassiveDefinition.Radius {
				continue
			}
			isTargetFound = true
			break
		}
	}
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	if !isTargetFound {
		if !e.run.transitionWaiting(fireTempestWaitingTarget) {
			return nil, nil
		}
		err := e.schedule(fireTempestTargetPoll, e.chargeReady)
		if err != nil {
			return nil, fmt.Errorf("firePassivePoll: %w", err)
		}
		return nil, nil
	}
	if !e.run.transitionWaiting(fireTempestHitPending) {
		return nil, nil
	}
	err := e.schedule(fireTempestHitDelay, e.hit)
	if err != nil {
		return nil, fmt.Errorf("firePassiveHit: %w", err)
	}
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: e.run.effectSlot + 1, IsRemovalRequested: true,
		IsHardStop: true, ObjectID: e.objectID,
	})
	if err != nil {
		return nil, fmt.Errorf("firePassiveChargeRemove: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e fireTempestPassiveSchedule) hit() ([][]byte, error) {
	if !e.run.transition(fireTempestHitPending, fireTempestRearming) {
		return nil, nil
	}
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || peerSession.zone == nil ||
		peerSession.zone.NPCs() == nil ||
		e.creatureID >= uint32(len(peerSession.binding.Creatures)) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	creature := peerSession.binding.Creatures[e.creatureID]
	center := game.Vec3(peerSession.playerPosition)
	plan, err := zoneability.PlanFixedArea(
		peerSession.zone.NPCs(), e.objectID, center, creature,
		fireTempestPassiveDefinition,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("firePassivePlan: %w", err)
	}
	results, err := zoneability.CommitArea(
		peerSession.zone.Population().Random(), peerSession.zone.NPCs(), plan,
		creature, peerSession.binding.Difficulty, e.runtime.program.Critical,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("firePassiveDamage: %w", err)
	}
	transitions := make([]campaignDamageTransition, 0, len(results))
	for index, result := range results {
		transition, transitionErr := peerSession.applyCampaignDamageTransition(result.Damage)
		if transitionErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("firePassiveTransition[%d]: %w", index, transitionErr)
		}
		transitions = append(transitions, transition)
	}
	binding := peerSession.binding
	sourcePosition := peerSession.playerPosition
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	err = e.schedule(fireTempestRearmDuration, e.rearm)
	if err != nil {
		return nil, fmt.Errorf("firePassiveRearm: %w", err)
	}
	timestamp := e.sourceTime + uint64(e.runtime.now().Sub(e.startedAt)/time.Millisecond)
	impact := campaignPointBlankImpact{
		assetName:  fireTempestPassiveDefinition.ImpactEffectName,
		attackerID: e.objectID, source: sourcePosition,
	}
	packets, err := e.runtime.damage.publishAreaResults(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
		binding, results, transitions, impact.marshal, true,
	)
	if err != nil {
		return nil, fmt.Errorf("firePassivePublish: %w", err)
	}
	stunPackets, err := e.runtime.damage.publishNPCStuns(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp, results,
		util.HashID("FireTempestStunModifier"), fireTempestStunDuration,
	)
	if err != nil {
		return nil, fmt.Errorf("firePassiveStun: %w", err)
	}
	return append(packets, stunPackets...), nil
}

func (e fireTempestPassiveSchedule) rearm() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent || !e.run.transition(fireTempestRearming, fireTempestCharging) {
		return nil, nil
	}
	err := e.schedule(fireTempestChargeDuration, e.chargeReady)
	if err != nil {
		return nil, fmt.Errorf("firePassiveCharge: %w", err)
	}
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: e.run.effectSlot + 1, IsForceAttached: true,
		Asset:    util.HashID(fireTempestPassiveDefinition.ActivationEffectName),
		ObjectID: e.objectID,
	})
	if err != nil {
		return nil, fmt.Errorf("firePassiveChargeEffect: %w", err)
	}
	return [][]byte{packet}, nil
}

func (r fireTempestPassiveRuntime) start(
	packet raknet.Packet, sessionKey string, generation uint64,
) ([]byte, error) {
	if r.registry == nil || r.effectPool == nil || r.now == nil {
		return nil, errors.New("fire passive runtime unavailable")
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures))
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	creature := peerSession.binding.Creatures[peerSession.deployedCreatureIndex]
	if creature.PassiveAbility != util.HashID("FireTempestPassive") {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.fireTempestPassive != nil {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	effectSlot, isAllocated := r.effectPool.Allocate(peerSession.deployedObjectID)
	if !isAllocated {
		r.registry.mutex.Unlock()
		return nil, errors.New("fire passive effect unavailable")
	}
	run := &fireTempestPassiveRun{
		effectPool: r.effectPool, objectID: peerSession.deployedObjectID,
		effectSlot: effectSlot, phase: fireTempestCharging,
	}
	peerSession.fireTempestPassive = run
	creatureID := peerSession.deployedCreatureIndex
	objectID := peerSession.deployedObjectID
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	startedAt := r.now()
	schedule := fireTempestPassiveSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey, generation: generation,
		objectID: objectID, creatureID: creatureID, sourceTime: packet.SourceTime,
		startedAt: startedAt, run: run,
	}
	err := schedule.schedule(fireTempestChargeDuration, schedule.chargeReady)
	if err != nil {
		run.Stop()
		r.registry.mutex.Lock()
		latest, isLatestFound := r.registry.sessions[sessionKey]
		if isLatestFound && latest.generation == generation &&
			latest.fireTempestPassive == run {
			latest.fireTempestPassive = nil
			r.registry.sessions[sessionKey] = latest
		}
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("firePassiveStart: %w", err)
	}
	chargePacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: effectSlot + 1, IsForceAttached: true,
		Asset:    util.HashID(fireTempestPassiveDefinition.ActivationEffectName),
		ObjectID: objectID,
	})
	if err != nil {
		run.Stop()
		r.registry.mutex.Lock()
		latest, isLatestFound := r.registry.sessions[sessionKey]
		if isLatestFound && latest.generation == generation &&
			latest.fireTempestPassive == run {
			latest.fireTempestPassive = nil
			r.registry.sessions[sessionKey] = latest
		}
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("firePassiveEffect: %w", err)
	}
	return chargePacket, nil
}
