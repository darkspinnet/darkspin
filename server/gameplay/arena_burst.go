package gameplay

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Directed bursts keep the shared launch/collision schedule and target geometry.
// Player defenses and healing statistics are committed against current owners.
func (e heroBurstStep) finishArenaTargetedBurstLocked(member gameplayPeerSession, targets *arenaAbilityTargets,
	target zonenpc.Snapshot, result zoneability.BasicResult, position sim.Position, facing sim.Position,
) ([][]byte, error) {
	schedule := e.schedule
	packets, err := schedule.run.ResolveCollision(context.Background(), e.index, e.deadline,
		true, true, result.Damage.PreviousHealth, result.Damage.Damage, result.IsCritical, position, facing)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("targetedBurstResolve: %w", err)
	}
	var healingPackets [][]byte
	healedAmount := float32(0)
	current, isFound := schedule.runtime.registry.sessions[schedule.sessionKey]
	if schedule.isCurrent(current, isFound) {
		var healingErr error
		healingPackets, healedAmount, healingErr = schedule.applyLifeSteal(&current, result.Damage.Damage)
		if healingErr != nil && schedule.runtime.logger != nil {
			schedule.runtime.logger.Printf("Arena burst healing source=%d: %v", schedule.sourceObjectID, healingErr)
		}
		schedule.runtime.registry.sessions[schedule.sessionKey] = current
		match := schedule.runtime.registry.arenaMatches[member.binding.GameID]
		if match != nil && match.isCombatActive && healedAmount > 0 {
			match.result.recordHealing(schedule.binding.UserID, schedule.binding.UserID, healedAmount)
		}
		schedule.runtime.registry.queueArenaAbilityResourcesLocked(current, healingPackets)
	}
	statusErr := targets.applyFireVulnerabilityLocked(result.Damage,
		schedule.packet.SourceTime+uint64(e.deadline/time.Millisecond))
	if statusErr != nil && schedule.runtime.logger != nil {
		schedule.runtime.logger.Printf("Arena Twinblaze vulnerability target=%d: %v", result.Damage.ObjectID, statusErr)
	}
	schedule.runtime.registry.mutex.Unlock()
	targets.finish()
	hitPackets, hitErr := targets.packets([]zoneability.AreaResult{{Snapshot: target, Damage: result.Damage,
		IsCritical: result.IsCritical, Definition: schedule.definition}})
	if hitErr != nil {
		return nil, fmt.Errorf("targetedBurstPublish: %w", hitErr)
	}
	if schedule.runtime.logger != nil {
		schedule.runtime.logger.Printf("Arena targeted burst impact ability=%s shot=%d source=%d target=%d damage=%g healing=%g deadline_ms=%d",
			schedule.definition.Name, e.index, schedule.sourceObjectID, target.Plan.ObjectID,
			result.Damage.Damage, healedAmount, e.deadline.Milliseconds())
	}
	packets = append(filterProjectileCombatPackets(packets), hitPackets...)
	return append(packets, healingPackets...), nil
}

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
			if schedule.definition.Name == "PlasmaRandom_WebbedLightning" &&
				!result.Damage.IsDamageImmune && !result.Damage.IsDefeated && result.Damage.Damage > 0 {
				schedule.webbedStatusTargets[targetID] = true
			}
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
	if err == nil {
		for _, result := range results {
			statusErr := targets.applyFireVulnerabilityLocked(result.Damage, timestamp)
			if statusErr != nil && schedule.runtime.logger != nil {
				schedule.runtime.logger.Printf("Arena Twinblaze vulnerability target=%d: %v", result.Damage.ObjectID, statusErr)
			}
		}
	}
	webbedTargetObjectIDs := schedule.webbedTargetObjectIDs(member)
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
	statusPackets, statusErr := e.applyWebbedStatus(webbedTargetObjectIDs)
	if statusErr != nil {
		return nil, fmt.Errorf("burstWebbedStatus: %w", statusErr)
	}
	packets = append(filterProjectileCombatPackets(packets), hitPackets...)
	return append(packets, statusPackets...), nil
}
