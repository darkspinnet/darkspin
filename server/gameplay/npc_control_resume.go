package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// One pending continuation belongs to the action that lost control. Its live
// status checks observe wake/dispel and extensions without reviving an action
// replaced by another owner, zone or action generation.
type campaignNPCControlResume struct {
	runtime          campaignNPCPursuitRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	instance         *zone.Zone
	npc              *zonenpc.Session
	owner            zonenpc.ActionOwner
	actionGeneration uint64
	objectID         uint32
	timestamp        uint64
	deferredAt       time.Time
	resumeAction     campaignNPCArrival
	resumeCommit     func() ([][]byte, error)
}

func (e *campaignNPCControlResume) remaining() (time.Duration, bool) {
	e.runtime.registry.mutex.RLock()
	defer e.runtime.registry.mutex.RUnlock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.zone == nil || member.zone.NPCs() == nil {
		return 0, false
	}
	if e.instance != nil && (member.zone != e.instance || member.zone.NPCs() != e.npc) {
		return 0, false
	}
	npc := member.zone.NPCs()
	actor, isActorFound := npc.NPC(e.objectID)
	owner := zonenpc.ActionOwner{UserID: member.binding.UserID, PeerGeneration: e.generation}
	if !isActorFound || actor.IsDefeated || actor.HitPoint <= 0 || !actor.IsActionStarted || actor.ActionOwner != owner {
		return 0, false
	}
	if !member.isCampaignNPCSourceGenerationActive(e.generation, e.objectID, actor.ActionGeneration) {
		return 0, false
	}
	if e.actionGeneration != 0 && (actor.ActionGeneration != e.actionGeneration || actor.ActionOwner != e.owner) {
		return 0, false
	}
	if e.instance == nil {
		e.instance = member.zone
		e.npc = npc
		e.owner = owner
		e.actionGeneration = actor.ActionGeneration
	}
	now := e.runtime.now()
	remaining := max(npc.StunRemaining(e.objectID, now), npc.SleepRemaining(e.objectID, now))
	if e.resumeAction != nil {
		remaining = max(remaining, npc.SilenceRemaining(e.objectID, now))
	}
	return remaining, true
}

func (e campaignNPCControlResume) deferControl() (bool, error) {
	remaining, isCurrent := e.remaining()
	if !isCurrent {
		// Consume an obsolete invocation instead of letting it continue into
		// another action's admission after its owner has already changed.
		return true, nil
	}
	if remaining <= 0 {
		return false, nil
	}
	e.deferredAt = e.runtime.now()
	err := e.schedule(remaining)
	if err != nil {
		e.release()
		return false, fmt.Errorf("controlSchedule: %w", err)
	}
	return true, nil
}

func (e campaignNPCControlResume) schedule(remaining time.Duration) error {
	if e.packet.ScheduleProducers == nil {
		return errors.New("control producer scheduler unavailable")
	}
	producers := e.runtime.registry.producerGuard.scheduledProducers(e.sessionKey, []raknet.ScheduledPacketProducer{{
		Delay: min(remaining, campaignPursuitFallbackTick), Produce: e.produce,
	}})
	cancel, err := e.packet.ScheduleProducers(producers)
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return fmt.Errorf("controlProducer: %w", err)
	}
	return nil
}

func (e campaignNPCControlResume) produce() ([][]byte, error) {
	remaining, isCurrent := e.remaining()
	if !isCurrent {
		return nil, nil
	}
	if remaining > 0 {
		err := e.schedule(remaining)
		if err != nil {
			e.release()
			return nil, fmt.Errorf("controlRetry: %w", err)
		}
		return nil, nil
	}
	var packets [][]byte
	var err error
	if e.resumeAction != nil {
		wait := max(time.Duration(0), e.runtime.now().Sub(e.deferredAt))
		packets, err = e.resumeAction(e.timestamp + uint64(wait/time.Millisecond))
	} else {
		packets, err = e.resumeCommit()
	}
	if err != nil {
		e.release()
		return packets, fmt.Errorf("controlResume: %w", err)
	}
	return packets, nil
}

func (e campaignNPCControlResume) release() {
	if e.npc == nil || e.actionGeneration == 0 {
		return
	}
	// This exact-generation operation cannot release a replacement action.
	isReleased := e.npc.ReleaseActionGeneration(e.objectID, e.owner, e.actionGeneration)
	if !isReleased {
		// The captured action already ended or was replaced before failure.
		return
	}
}
