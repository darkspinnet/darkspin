package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type stackingBurnExpiry struct {
	runtime    campaignNPCActionRuntime
	sessionKey string
	generation uint64
	run        *campaignNPCPoisonRun
	revision   uint64
	deadlineAt time.Time
}

// Build-103 chunk503 increments a stack and resets duration. Mushroom472 only
// resets duration. Both retain the DOT thread's original authored tick cadence.
func (e campaignNPCPoisonStep) scheduleStackingBurnTick() error {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.campaignNPCPoisons[e.run.key] != e.run {
		return nil
	}
	next := e
	next.tickIndex++
	next.timestamp = e.run.stackingTickTimestamp
	next.stackingTickAt = e.run.stackingTickAt
	delay := max(time.Duration(0), next.stackingTickAt.Sub(e.runtime.now()))
	cancel, err := scheduleNPCProducers(e.runtime.registry, e.run.packet,
		[]raknet.ScheduledPacketProducer{{Delay: delay, Produce: next.produce}})
	if err != nil {
		return fmt.Errorf("burnSchedule: %w", err)
	}
	if cancel == nil {
		return fmt.Errorf("burnSchedule: unavailable")
	}
	e.run.cancel = cancel
	return nil
}

func (e campaignNPCActionRuntime) scheduleStackingBurnExpiryLocked(packet raknet.Packet,
	sessionKey string, generation uint64, run *campaignNPCPoisonRun, timestamp uint64,
) error {
	revision := run.stackingExpiryRevision + 1
	expiry := stackingBurnExpiry{runtime: e, sessionKey: sessionKey, generation: generation,
		run: run, revision: revision, deadlineAt: e.now().Add(run.plan.Profile.ModifierDuration)}
	// Keep the final damage tick ahead of expiry even if equal-deadline
	// producers are executed in arbitrary order by the transport scheduler.
	cancel, err := scheduleNPCProducers(e.registry, packet,
		[]raknet.ScheduledPacketProducer{{Delay: run.plan.Profile.ModifierDuration + time.Millisecond, Produce: expiry.produce}})
	if err != nil {
		return fmt.Errorf("burnExpirySchedule: %w", err)
	}
	if cancel == nil {
		return fmt.Errorf("burnExpirySchedule: unavailable")
	}
	if run.stackingExpiryCancel != nil {
		run.stackingExpiryCancel()
	}
	run.stackingExpiryCancel = cancel
	run.stackingExpiryRevision = revision
	return nil
}

func (e campaignNPCActionRuntime) refreshStackingBurnLocked(packet raknet.Packet,
	sessionKey string, generation uint64, run *campaignNPCPoisonRun,
	plan zonenpc.AttackPlan, timestamp uint64,
) ([][]byte, error) {
	stackCount := min(plan.Profile.ModifierMaximumStack, run.stackCount+1)
	created, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: plan.SourceObjectID, TargetObjectID: plan.TargetObjectID,
		ModifierID: plan.Profile.ModifierGUID(), InstanceID: run.modifier.instanceID,
		StackCount: stackCount, Duration: plan.Profile.ModifierDuration, Timestamp: timestamp})
	if err != nil {
		return nil, fmt.Errorf("burnStack: %w", err)
	}
	previousPlan := run.plan
	plan.Profile.DescriptorMask = plan.Profile.ModifierDescriptorMask
	plan.Profile.DamageType = plan.Profile.ModifierDamageType
	plan.Profile.DamageSource = plan.Profile.ModifierDamageSource
	plan.Profile.IsDamageProfileKnown = true
	run.plan = plan
	err = e.scheduleStackingBurnExpiryLocked(packet, sessionKey, generation, run, timestamp)
	if err != nil {
		run.plan = previousPlan
		return nil, fmt.Errorf("burnRefreshExpiry: %w", err)
	}
	run.stackCount = stackCount
	return [][]byte{created}, nil
}

func (e stackingBurnExpiry) produce() ([][]byte, error) {
	packets := make([][]byte, 0)
	for {
		e.runtime.registry.mutex.Lock()
		member, isFound := e.runtime.registry.sessions[e.sessionKey]
		isCurrent := isFound && member.generation == e.generation &&
			member.campaignNPCPoisons[e.run.key] == e.run && e.run.stackingExpiryRevision == e.revision
		if !isCurrent {
			e.runtime.registry.mutex.Unlock()
			return packets, nil
		}
		isPending := !e.run.stackingTickAt.IsZero() && !e.run.stackingTickAt.After(e.deadlineAt)
		step := campaignNPCPoisonStep{runtime: e.runtime, sessionKey: e.sessionKey, generation: e.generation,
			timestamp: e.run.stackingTickTimestamp, revision: e.run.revision, run: e.run,
			stackingTickAt: e.run.stackingTickAt}
		e.runtime.registry.mutex.Unlock()
		if !isPending {
			break
		}
		// A delayed callback may reach expiry before the last due damage
		// producer. Consume each retained deadline once before deletion.
		tickPackets, err := step.produce()
		if err != nil {
			return packets, fmt.Errorf("burnFinalTick: %w", err)
		}
		packets = append(packets, tickPackets...)
	}
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.generation == e.generation &&
		member.campaignNPCPoisons[e.run.key] == e.run && e.run.stackingExpiryRevision == e.revision
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return packets, nil
	}
	delete(member.campaignNPCPoisons, e.run.key)
	member.untrackCampaignNPCModifier(e.run.modifier)
	if e.run.cancel != nil {
		e.run.cancel()
	}
	e.runtime.registry.sessions[e.sessionKey] = member
	e.runtime.registry.mutex.Unlock()
	isCreated, err := e.run.modifier.release(e.runtime.modifierPool)
	if err != nil {
		return packets, fmt.Errorf("burnRelease: %w", err)
	}
	if !isCreated {
		return packets, nil
	}
	deleted, err := effectraknet.ModifierDelete(e.run.plan.TargetObjectID, e.run.modifier.instanceID)
	if err != nil {
		return packets, fmt.Errorf("burnDelete: %w", err)
	}
	return append(packets, deleted), nil
}
