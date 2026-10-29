package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type campaignNashiraCurseStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	timestamp  uint64
	plan       zonenpc.AttackPlan
}

func (e campaignNPCActionRuntime) produceNashiraCurse(
	packet raknet.Packet, sessionKey string, generation uint64, objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || !member.isCampaignNPCSourceActive(generation, objectID) {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	source, isSourceFound := member.zone.NPCs().NPC(objectID)
	profile, isProfileFound := zonenpc.NashiraCurseProfile(source.Plan)
	target, isTargetFound := member.campaignNPCTarget(generation, source.TargetObjectID)
	if !isSourceFound || !isProfileFound || !isTargetFound || source.Plan.OwnerObjectID != 0 ||
		timestamp < member.campaignNashiraCurseReadiness[objectID] ||
		member.zone.NPCs().SilenceRemaining(objectID, e.now()) > 0 {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	key := campaignNPCPoisonKey{targetObjectID: target.ObjectID, modifierName: profile.ModifierName}
	if member.campaignNPCPoisons[key] != nil {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	plan, err := zonenpc.PlanControlWithProfile(source, target.ObjectID, target.Position,
		profile, target.ActorFootprintRadius)
	if err != nil {
		e.registry.mutex.Unlock()
		if errors.Is(err, zonenpc.ErrControlTargetOutOfRange) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("cursePlan: %w", err)
	}
	if member.campaignNashiraCurseReadiness == nil {
		member.campaignNashiraCurseReadiness = make(map[uint32]uint64)
	}
	previous := member.campaignNashiraCurseReadiness[objectID]
	member.campaignNashiraCurseReadiness[objectID] = timestamp + uint64((profile.HitDelay+profile.Cooldown)/time.Millisecond)
	e.registry.sessions[sessionKey] = member
	e.registry.mutex.Unlock()
	packets, err := e.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		e.restoreNashiraCurseReadiness(sessionKey, generation, objectID, previous)
		return nil, true, fmt.Errorf("curseStart: %w", err)
	}
	step := campaignNashiraCurseStep{runtime: e, packet: packet, sessionKey: sessionKey,
		generation: generation, timestamp: timestamp, plan: plan}
	cancel, err := scheduleNPCProducers(e.registry, packet, []raknet.ScheduledPacketProducer{
		{Delay: profile.HitDelay, Produce: step.hit},
		{Delay: profile.ReleaseDelay, Produce: step.next},
	})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		e.restoreNashiraCurseReadiness(sessionKey, generation, objectID, previous)
		e.releaseActionGeneration(sessionKey, generation, objectID, source.ActionGeneration)
		return nil, true, fmt.Errorf("curseSchedule: %w", err)
	}
	return packets, true, nil
}

func (e campaignNPCActionRuntime) restoreNashiraCurseReadiness(
	sessionKey string, generation uint64, objectID uint32, timestamp uint64,
) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation {
		return
	}
	member.campaignNashiraCurseReadiness[objectID] = timestamp
	e.registry.sessions[sessionKey] = member
}

func (e campaignNashiraCurseStep) hit() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.isCampaignNPCAttackGenerationActiveAt(e.generation,
		e.plan.SourceObjectID, e.plan.TargetObjectID, e.plan.ActionGeneration, e.runtime.now())
	if isCurrent {
		isCurrent = member.zone.NPCs().SilenceRemaining(e.plan.SourceObjectID, e.runtime.now()) == 0
	}
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets, err := e.runtime.applyCampaignNPCPoison(e.packet, e.sessionKey, e.generation,
		e.plan, e.timestamp+uint64(e.plan.Profile.HitDelay/time.Millisecond))
	if err != nil {
		return nil, fmt.Errorf("curseApply: %w", err)
	}
	return packets, nil
}

func (e campaignNashiraCurseStep) next() ([][]byte, error) {
	step := campaignNPCFirstActionStep{runtime: e.runtime, packet: e.packet,
		sessionKey: e.sessionKey, generation: e.generation, objectID: e.plan.SourceObjectID,
		actionGeneration: e.plan.ActionGeneration,
		timestamp:        e.timestamp + uint64(e.plan.Profile.ReleaseDelay/time.Millisecond)}
	packets, err := step.produce()
	if err != nil {
		return nil, fmt.Errorf("curseNext: %w", err)
	}
	return packets, nil
}

// Called under the registry lock on source defeat, or for all sources on teardown.
func (e *gameplayPeerSession) stopNashiraCurses(sourceObjectID uint32, pool *modifierPool) ([][]byte, error) {
	packets := make([][]byte, 0)
	for key, run := range e.campaignNPCPoisons {
		if key.modifierName != "ShadowCurse" ||
			(sourceObjectID != 0 && run.plan.SourceObjectID != sourceObjectID) {
			continue
		}
		delete(e.campaignNPCPoisons, key)
		e.untrackCampaignNPCModifier(run.modifier)
		if run.cancel != nil {
			run.cancel()
		}
		stopPackets, err := releaseNashiraCurse(run, pool)
		if err != nil {
			return packets, fmt.Errorf("curseStop: %w", err)
		}
		packets = append(packets, stopPackets...)
	}
	return packets, nil
}

func releaseNashiraCurse(run *campaignNPCPoisonRun, pool *modifierPool) ([][]byte, error) {
	isCreated, err := run.modifier.release(pool)
	if err != nil {
		return nil, fmt.Errorf("curseRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(run.plan.TargetObjectID, run.modifier.instanceID)
	if err != nil {
		return nil, fmt.Errorf("curseDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e *gameplayPeerSession) stopNashiraCurseTarget(targetObjectID uint32, pool *modifierPool) ([][]byte, error) {
	key := campaignNPCPoisonKey{targetObjectID: targetObjectID, modifierName: "ShadowCurse"}
	run := e.campaignNPCPoisons[key]
	if run == nil {
		return nil, nil
	}
	delete(e.campaignNPCPoisons, key)
	e.untrackCampaignNPCModifier(run.modifier)
	if run.cancel != nil {
		run.cancel()
	}
	packets, err := releaseNashiraCurse(run, pool)
	if err != nil {
		return nil, fmt.Errorf("curseTargetStop: %w", err)
	}
	return packets, nil
}
