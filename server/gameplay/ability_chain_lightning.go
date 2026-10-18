package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

type campaignChainLightningStep struct {
	schedule         campaignMeleeSchedule
	plan             zoneability.BasicPlan
	previousObjectID uint32
	previousPosition game.Vec3
	visitedObjectIDs []uint32
	hopIndex         uint32
	deadline         time.Duration
}

// TargetUtils emits the beam with objectId=previous and
// secondaryObjectId=hit target; the hit event attaches to the target itself.
func campaignChainLightningEffects(
	plan zoneability.BasicPlan, previousObjectID uint32, hopIndex uint32,
) ([][]byte, error) {
	names := plan.Definition.ChainLightning.EffectNames
	if len(names) == 0 {
		return nil, nil
	}
	index := min(int(hopIndex), len(names)-1)
	packets, err := effectraknet.ChainHit(effectraknet.ChainHitRequest{
		EffectID:       util.HashID(names[index]),
		HitEffectID:    util.HashID(plan.Definition.HitEffectName),
		SourceObjectID: previousObjectID, TargetObjectID: plan.TargetObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("chainEffects: %w", err)
	}
	return packets, nil
}

func (e campaignChainLightningStep) scheduleNext() error {
	chain := e.plan.Definition.ChainLightning
	if e.hopIndex >= chain.MaximumHopCount {
		return nil
	}
	if e.schedule.packet.ScheduleFunc == nil {
		return errors.New("chain scheduler unavailable")
	}
	e.hopIndex++
	e.deadline += chain.HopDelay
	err := e.schedule.packet.ScheduleFunc(chain.HopDelay, e.produce)
	if err != nil {
		return fmt.Errorf("chainSchedule: %w", err)
	}
	return nil
}

func (e campaignChainLightningStep) produce() ([][]byte, error) {
	schedule := e.schedule
	runtime := schedule.runtime
	runtime.registry.mutex.Lock()
	current, isFound := runtime.registry.sessions[schedule.sessionKey]
	if !isFound || current.generation != schedule.generation ||
		current.deployedObjectID != schedule.sourceObjectID ||
		current.zone == nil || !current.zone.IsActive() || current.isZoneTerminal() ||
		current.zone.NPCs() == nil || current.zone.Population() == nil {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	plan, target, isTargetFound, err := zoneability.PlanChainLightningHop(
		current.zone.NPCs(), zoneability.ChainLightningHopRequest{
			Plan: e.plan, Creature: schedule.creature,
			PreviousPosition: e.previousPosition,
			VisitedObjectIDs: e.visitedObjectIDs, HopIndex: e.hopIndex,
		})
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("chainPlan: %w", err)
	}
	if !isTargetFound {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	result, err := zoneability.CommitBasic(current.zone.Population().Random(),
		current.zone.NPCs(), plan, schedule.creature,
		current.binding.Difficulty, runtime.program.Critical)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("chainCommit: %w", err)
	}
	transition, err := current.applyCampaignDamageTransition(result.Damage)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("chainTransition: %w", err)
	}
	runtime.registry.sessions[schedule.sessionKey] = current
	runtime.registry.mutex.Unlock()
	results := []zoneability.AreaResult{{
		Snapshot: target, Damage: result.Damage,
		IsCritical: result.IsCritical, Definition: plan.Definition,
	}}
	timestamp := schedule.packet.SourceTime + uint64(e.deadline/time.Millisecond)
	packets, err := runtime.damage.publishAreaResults(
		schedule.packet, schedule.sessionKey, schedule.generation,
		schedule.sourceObjectID, timestamp, schedule.binding, results,
		[]campaignDamageTransition{transition}, nil, true)
	if err != nil {
		return nil, fmt.Errorf("chainPublish: %w", err)
	}
	effectPackets, err := campaignChainLightningEffects(plan, e.previousObjectID, e.hopIndex)
	if err != nil {
		return nil, fmt.Errorf("chainPresent: %w", err)
	}
	packets = append(packets, effectPackets...)
	e.previousObjectID = target.Plan.ObjectID
	e.previousPosition = target.Plan.Position
	e.visitedObjectIDs = append(e.visitedObjectIDs, target.Plan.ObjectID)
	err = e.scheduleNext()
	if err != nil {
		runtime.logger.Printf("RakNet Chain Lightning continuation omitted source=%d: %v", schedule.sourceObjectID, err)
	}
	return packets, nil
}

func (e campaignMeleeSchedule) startChainLightning(
	plan zoneability.BasicPlan, position game.Vec3, delay time.Duration,
) ([][]byte, error) {
	packets, err := campaignChainLightningEffects(plan, e.sourceObjectID, 0)
	if err != nil {
		return nil, fmt.Errorf("chainPrimary: %w", err)
	}
	step := campaignChainLightningStep{
		schedule: e, plan: plan, previousObjectID: plan.TargetObjectID,
		previousPosition: position, visitedObjectIDs: []uint32{plan.TargetObjectID},
		deadline: delay,
	}
	err = step.scheduleNext()
	if err != nil {
		return packets, fmt.Errorf("chainStart: %w", err)
	}
	return packets, nil
}
