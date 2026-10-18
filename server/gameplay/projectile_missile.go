package gameplay

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// MissileTempestBasic's authored validator keeps a live visible lock, otherwise
// searches ten meters ahead within 45 degrees, excluding destructibles.
func (e campaignProjectileSchedule) trackMissileTarget(
	member gameplayPeerSession, now time.Time,
) ([][]byte, error) {
	snapshot := e.run.Snapshot(now)
	if !snapshot.IsActive || snapshot.IsFrozen {
		return nil, nil
	}
	targetID := uint32(0)
	aim := sim.Position{}
	nearest := float32(10)
	for _, target := range member.zone.NPCs().LiveSnapshots() {
		if !target.IsPublished || target.IsDefeated || target.HitPoint <= 0 ||
			target.Plan.IsFixture || target.Faction != zonenpc.FactionNonPlayerAligned ||
			target.IsSpawnStealthActive || member.zone.NPCs().IsStealthed(target.Plan.ObjectID) {
			continue
		}
		geometry, isFallback, err := campaignTargetProjectileGeometry(e.runtime.program, target)
		if err != nil {
			return nil, fmt.Errorf("missileGeometry: %w", err)
		}
		// The shared geometry resolver supplies its conservative bounds when
		// authored bounds are absent; both aiming and collision use those bounds.
		if isFallback && geometry.TargetMinimum == geometry.TargetMaximum {
			continue
		}
		position := campaignProjectileAimPosition(target.Plan.Position,
			target.Plan.NPCProfile.FootprintRadius, geometry)
		if target.Plan.ObjectID == snapshot.TargetObjectID {
			targetID, aim = target.Plan.ObjectID, sim.Position(position)
			break
		}
		delta := position.Sub(game.Vec3(snapshot.Position))
		distance := delta.Length()
		if distance <= 0 || distance > nearest {
			continue
		}
		dot := delta.X*snapshot.Direction.X + delta.Y*snapshot.Direction.Y + delta.Z*snapshot.Direction.Z
		if dot/distance < float32(math.Cos(math.Pi/4)) {
			continue
		}
		if distance == nearest && targetID != 0 && target.Plan.ObjectID > targetID {
			continue
		}
		nearest, targetID, aim = distance, target.Plan.ObjectID, sim.Position(position)
	}
	packets, err := e.run.Retarget(now, targetID, aim, 6)
	if err != nil {
		return nil, fmt.Errorf("missileRetarget: %w", err)
	}
	return packets, nil
}

// Entered with the registry locked. The explosion replaces direct-hit damage,
// so the struck enemy receives one damage application, just like its neighbors.
func (e campaignProjectileStep) produceMissileExplosion(
	member gameplayPeerSession, position sim.Position, struckObjectID uint32,
) ([][]byte, error) {
	schedule := e.schedule
	radius, err := zoneability.ProjectAreaRadius(schedule.creature, schedule.projectile.Radius)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("missileRadius: %w", err)
	}
	targets := make([]zonenpc.Snapshot, 0)
	for _, target := range member.zone.NPCs().LiveSnapshots() {
		if !target.IsPublished || target.IsDefeated || target.HitPoint <= 0 ||
			!isSupportHealerBasicImpactTarget(target) {
			continue
		}
		// MissileTempestBasic's hitAreaCallback queries overlapping spatial
		// bounds (127 A0AED0 -> 9D9300 -> A162F0), not just object centers.
		// Preserve the actual collision target: a destructor's surface can be
		// farther than the three-unit blast radius from its object origin.
		if target.Plan.ObjectID != struckObjectID &&
			target.Plan.Position.Sub(game.Vec3(position)).Length() > radius {
			continue
		}
		targets = append(targets, target)
	}
	plan := zoneability.AreaPlan{
		SourceObjectID: schedule.sourceObjectID, AbilityID: schedule.plan.AbilityID,
		Definition: schedule.projectile, Damage: schedule.plan.Damage,
		Center: game.Vec3(position), Target: targets,
	}
	results, err := zoneability.CommitArea(member.zone.Population().Random(),
		member.zone.NPCs(), plan, schedule.creature, member.binding.Difficulty,
		schedule.runtime.program.Critical)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("missileDamage: %w", err)
	}
	transitions := make([]campaignDamageTransition, 0, len(results))
	for _, result := range results {
		transition, transitionErr := member.applyCampaignDamageTransition(result.Damage)
		if transitionErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("missileTransition: %w", transitionErr)
		}
		transitions = append(transitions, transition)
	}
	packets, err := schedule.run.ResolveCollision(context.Background(), e.deadline,
		false, false, 0, schedule.plan.Damage.Maximum, false, position, sim.Position(schedule.facing))
	e.finishFlightLocked(&member)
	schedule.runtime.registry.sessions[schedule.sessionKey] = member
	schedule.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("missileExplosion: %w", err)
	}
	resultPackets, err := schedule.runtime.damage.publishAreaResults(
		schedule.packet, schedule.sessionKey, schedule.generation, schedule.sourceObjectID,
		schedule.packet.SourceTime+uint64(e.deadline/time.Millisecond), schedule.binding,
		results, transitions, nil, false)
	if err != nil {
		return nil, fmt.Errorf("missilePublish: %w", err)
	}
	return append(filterProjectileCombatPackets(packets), resultPackets...), nil
}
