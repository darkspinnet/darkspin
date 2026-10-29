package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type additionalInstantHit struct {
	step additionalKitStep
	plan zonenpc.AttackPlan
}

// InstantNova's authored damage pair is exactly zero. The instant-cast template
// admits one control target and requests Chilled after the one-second windup.
// No cooldown is assigned by this class/template; its release gates repetition.
func (e additionalKitStep) startInstant(profile zonenpc.ActionProfile, actor zonenpc.Snapshot) ([][]byte, error) {
	profile = zonenpc.ApplyAffixTiming(profile, actor.Plan)
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || !member.isCampaignNPCSourceGenerationActive(e.generation, e.objectID, actor.ActionGeneration) {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	target, isTargetFound := member.campaignNPCTarget(e.generation, actor.TargetObjectID)
	e.runtime.registry.mutex.RUnlock()
	if !isTargetFound {
		return nil, nil
	}
	plan, err := zonenpc.PlanControlWithProfile(actor, target.ObjectID, target.Position, profile, target.ActorFootprintRadius)
	if err != nil {
		if !errors.Is(err, zonenpc.ErrControlTargetOutOfRange) {
			return nil, fmt.Errorf("instantPlan: %w", err)
		}
		action, actionErr := campaignNPCActionWithProfile(actor.Plan, target.ObjectID, target.Position, profile, target.ActorFootprintRadius)
		if actionErr != nil {
			return nil, fmt.Errorf("instantPursuitPlan: %w", actionErr)
		}
		if !action.IsPursuitNeeded {
			return nil, nil
		}
		packets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			return nil, fmt.Errorf("instantPursuitMarshal: %w", marshalErr)
		}
		scheduleErr := e.runtime.pursuit.schedule(e.packet, e.sessionKey, e.generation, e.objectID, e.timestamp, action.TargetPosition, profile, e.resume)
		if scheduleErr != nil {
			return nil, fmt.Errorf("instantPursuitSchedule: %w", scheduleErr)
		}
		return packets, nil
	}
	packets, err := e.runtime.startNPCAttack(e.sessionKey, e.generation, plan, e.timestamp)
	if err != nil {
		return nil, fmt.Errorf("instantStart: %w", err)
	}
	hit := additionalInstantHit{step: e, plan: plan}
	e.timestamp += uint64(profile.ReleaseDelay / time.Millisecond)
	cancel, err := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{Delay: profile.HitDelay, Produce: hit.produce}, {Delay: profile.ReleaseDelay, Produce: e.next}})
	if err != nil {
		return nil, fmt.Errorf("instantSchedule: %w", err)
	}
	if cancel == nil {
		return nil, fmt.Errorf("instantSchedule: unavailable")
	}
	return packets, nil
}

func (e additionalInstantHit) produce() ([][]byte, error) {
	runtime := e.step.runtime
	runtime.registry.mutex.RLock()
	member, isFound := runtime.registry.sessions[e.step.sessionKey]
	isCurrent := isFound && member.isCampaignNPCAttackGenerationActiveAt(e.step.generation, e.step.objectID, e.plan.TargetObjectID, e.plan.ActionGeneration, runtime.now())
	if !isCurrent {
		runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	target, isTargetFound := member.campaignNPCTarget(e.step.generation, e.plan.TargetObjectID)
	runtime.registry.mutex.RUnlock()
	if !isTargetFound {
		return nil, nil
	}
	plan := e.plan
	plan.TargetPosition = target.Position
	packets, err := runtime.applyCampaignNPCTimedModifier(e.step.packet, e.step.sessionKey, e.step.generation, plan, e.step.timestamp+1000)
	if err != nil {
		return nil, fmt.Errorf("instantChilled: %w", err)
	}
	impact, err := npcraknet.AttackImpact(plan)
	if err != nil {
		return nil, fmt.Errorf("instantImpact: %w", err)
	}
	return append(packets, impact), nil
}
