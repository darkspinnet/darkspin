package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// stop is called under the registry lock by expiry and death/disconnect cleanup.
type polarisMarkPresentation struct {
	registry  *gameplaySessionRegistry
	zone      *zone.Zone
	pool      *attachedEffectPool
	objectID  uint32
	slot      uint8
	packets   [][]byte
	cancel    raknet.CancelSchedule
	isStopped bool
}

func (e *polarisMarkPresentation) stop() {
	if e.isStopped {
		return
	}
	e.isStopped = true
	if e.cancel != nil {
		e.cancel()
	}
	e.pool.Release(e.objectID, e.slot)
	for key, member := range e.registry.sessions {
		if member.zone != e.zone {
			continue
		}
		member.queueCampaignPackets(e.packets)
		e.registry.sessions[key] = member
	}
}

func (e campaignNPCActionRuntime) attachPolarisMark(
	sessionKey string, generation uint64, run *campaignNPCModifierRun, effectName string,
) ([][]byte, error) {
	startPacket, stopPackets, slot, err := preparePhantomChargeEffect(e.effectPool, run.record.TargetObjectID, effectName)
	if err != nil {
		return nil, fmt.Errorf("reticlePrepare: %w", err)
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.campaignNPCModifiers[run.instanceID] != run || !run.isActive() {
		e.effectPool.Release(run.record.TargetObjectID, slot)
		return nil, nil
	}
	presentation := &polarisMarkPresentation{registry: e.registry, zone: member.zone,
		pool: e.effectPool, objectID: run.record.TargetObjectID, slot: slot,
		packets: stopPackets, cancel: run.cancel}
	run.cancel = presentation.stop
	return [][]byte{startPacket}, nil
}

type campaignPolarisMarkStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	timestamp  uint64
	plan       zonenpc.AttackPlan
}

func (e campaignNPCActionRuntime) producePolarisMark(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || !member.isCampaignNPCSourceActive(generation, objectID) {
		e.registry.mutex.Unlock()
		return nil, true, nil
	}
	boss, isBossFound := member.zone.NPCs().NPC(objectID)
	profile, isProfileFound := zonenpc.ZelemMarkProfile(boss.Plan.NounName)
	state := member.campaignNPCPolarisStates[objectID]
	if !isBossFound || !isProfileFound || boss.IsDefeated || timestamp < state.nextMarkTimestamp ||
		member.zone.NPCs().SilenceRemaining(objectID, e.now()) > 0 {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	target, isTargetFound := member.campaignNPCTarget(generation, boss.TargetObjectID)
	// CheckZelemMark (chunk 842) substitutes a pet's owner before admission.
	if isTargetFound && !target.IsHero && member.zone.Companion() != nil {
		for _, motion := range member.zone.Companion().MotionSnapshots(e.now()) {
			if motion.Actor.ObjectID == target.ObjectID {
				target, isTargetFound = member.campaignNPCTarget(generation, motion.Actor.OwnerObjectID)
				break
			}
		}
	}
	if !isTargetFound {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	for _, modifier := range member.zone.Effect().Snapshot() {
		if modifier.GUID == profile.ModifierGUID() && modifier.TargetObjectID == target.ObjectID {
			e.registry.mutex.Unlock()
			return nil, false, nil
		}
	}
	// This gambit's condition explicitly overrides the selected pet with its owner.
	boss.TargetObjectID = target.ObjectID
	plan, err := zonenpc.PlanControlWithProfile(boss, target.ObjectID, target.Position, profile, target.ActorFootprintRadius)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	previous := state
	state.nextMarkTimestamp = timestamp + uint64(profile.Cooldown/time.Millisecond)
	if member.campaignNPCPolarisStates == nil {
		member.campaignNPCPolarisStates = make(map[uint32]campaignNPCPolarisState)
	}
	member.campaignNPCPolarisStates[objectID] = state
	e.registry.sessions[sessionKey] = member
	e.registry.mutex.Unlock()
	packets, err := e.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		e.restoreCampaignNPCPolarisState(sessionKey, generation, objectID, previous)
		return nil, true, fmt.Errorf("markStart: %w", err)
	}
	step := campaignPolarisMarkStep{runtime: e, packet: packet, sessionKey: sessionKey,
		generation: generation, timestamp: timestamp, plan: plan}
	cancel, err := scheduleNPCProducers(e.registry, packet, []raknet.ScheduledPacketProducer{
		{Delay: profile.HitDelay, Produce: step.hit},
		{Delay: max(profile.HitDelay, profile.ReleaseDelay), Produce: step.next},
	})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		e.restoreCampaignNPCPolarisState(sessionKey, generation, objectID, previous)
		e.releaseAction(sessionKey, generation, objectID)
		return nil, true, fmt.Errorf("markSchedule: %w", err)
	}
	return packets, true, nil
}

func (e campaignPolarisMarkStep) hit() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.isCampaignNPCSourceGenerationActive(e.generation,
		e.plan.SourceObjectID, e.plan.ActionGeneration)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets, err := e.runtime.applyCampaignNPCTimedModifier(e.packet, e.sessionKey,
		e.generation, e.plan, e.timestamp+uint64(e.plan.Profile.HitDelay/time.Millisecond))
	if err != nil {
		return nil, fmt.Errorf("markHit: %w", err)
	}
	return packets, nil
}

func (e campaignPolarisMarkStep) next() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.isCampaignNPCSourceGenerationActive(e.generation,
		e.plan.SourceObjectID, e.plan.ActionGeneration)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets, err := e.runtime.producePolarisSeeker(e.packet, e.sessionKey, e.generation,
		e.plan.SourceObjectID, e.timestamp+uint64(max(e.plan.Profile.HitDelay, e.plan.Profile.ReleaseDelay)/time.Millisecond))
	if err != nil {
		return nil, fmt.Errorf("markNext: %w", err)
	}
	return packets, nil
}
