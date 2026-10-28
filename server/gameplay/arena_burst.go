package gameplay

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
)

// The shared burst owns launch times, scatter, flight and retirement. Only
// the collision callback changes from an NPC to a real opposing squad member.
func (e heroBurstStep) produceArenaBurstImpact(member gameplayPeerSession) ([][]byte, error) {
	schedule := e.schedule
	timestamp := schedule.packet.SourceTime + uint64(e.deadline/time.Millisecond)
	targets := schedule.runtime.registry.abilityTargetsLocked(schedule.runtime, member, schedule.definition, timestamp).(*arenaAbilityTargets)
	position := schedule.shotTargetPositions[e.index]
	targetID := uint32(0)
	nearest := schedule.definition.Radius
	isRadial := schedule.definition.BurstTargeting == sim.ProjectileBurstTargetingRadial
	if isRadial {
		nearest = schedule.definition.Distance + schedule.definition.Radius
	}
	for _, target := range targets.LiveSnapshots() {
		if schedule.hitCounts[target.Plan.ObjectID] >= 4 {
			continue
		}
		distance := target.Plan.Position.Sub(game.Vec3(position)).Length()
		if isRadial {
			origin := schedule.shotSourcePositions[e.index]
			if chargeSegmentDistance(origin, game.Vec3(position), target.Plan.Position) >
				schedule.definition.Radius+target.Plan.NPCProfile.FootprintRadius {
				continue
			}
			distance = target.Plan.Position.Sub(origin).Length()
		}
		if distance > nearest {
			continue
		}
		nearest = distance
		targetID = target.Plan.ObjectID
	}
	results := make([]zoneability.AreaResult, 0, 1)
	previousHealth, damage := float32(0), schedule.plan.Damage.Maximum
	isCritical := false
	if targetID != 0 {
		target, isFound := targets.NPC(targetID)
		if isFound {
			plan := schedule.plan
			plan.TargetObjectID = targetID
			random, randomErr := schedule.runtime.registry.abilityRandomLocked(member)
			if randomErr != nil {
				schedule.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("burstRandom: %w", randomErr)
			}
			result, commitErr := zoneability.CommitBasic(random, targets, plan,
				schedule.creature, member.binding.Difficulty, schedule.runtime.program.Critical)
			if commitErr != nil {
				schedule.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("burstDamage: %w", commitErr)
			}
			schedule.hitCounts[targetID]++
			previousHealth, damage, isCritical = result.Damage.PreviousHealth, result.Damage.Damage, result.IsCritical
			position = raknet.Vector3(target.Plan.Position)
			results = append(results, zoneability.AreaResult{Snapshot: target, Damage: result.Damage,
				IsCritical: result.IsCritical, Definition: schedule.definition})
		}
	}
	facing := sim.Position{Z: -1}
	if isRadial {
		facing = sim.Position(zoneability.ProjectileDirection(raknet.Vector3(schedule.shotSourcePositions[e.index]), position))
	}
	packets, err := schedule.run.ResolveCollision(context.Background(), e.index, e.deadline,
		true, len(results) != 0, previousHealth, damage, isCritical, sim.Position(position), facing)
	schedule.runtime.registry.mutex.Unlock()
	targets.finish()
	if err != nil {
		return nil, fmt.Errorf("burstImpact: %w", err)
	}
	hitPackets, err := targets.packets(results)
	if err != nil {
		return nil, fmt.Errorf("burstPublish: %w", err)
	}
	if schedule.runtime.logger != nil {
		schedule.runtime.logger.Printf("Arena projectile burst impact ability=%s shot=%d target=%d deadline_ms=%d",
			schedule.definition.Name, e.index, targetID, e.deadline.Milliseconds())
	}
	return append(filterProjectileCombatPackets(packets), hitPackets...), nil
}
