package gameplay

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func arenaAbilityGameID(member gameplayPeerSession) uint32 {
	if member.binding.Mode == game.ModeArena {
		return member.binding.GameID
	}
	return 0
}

func (e *campaignProjectileRetirement) isMemberRealm(member gameplayPeerSession) bool {
	if e.arenaGameID != 0 {
		return member.binding.Mode == game.ModeArena && member.binding.GameID == e.arenaGameID
	}
	return e.originalZone != nil && member.zone == e.originalZone
}

func (e *gameplaySessionRegistry) queueArenaProjectilePacketsLocked(req *campaignProjectileRetirement, ownerSessionKey string, packets [][]byte) {
	presentations := gameplayPeerPresentationPackets(packets)
	for key, member := range e.sessions {
		if !req.isMemberRealm(member) || member.isRejoinPending || !member.stage.IsDungeon() {
			continue
		}
		if key == ownerSessionKey && member.generation == req.generation {
			member.queuePackets(packets)
		} else {
			member.queuePackets(presentations)
		}
		e.sessions[key] = member
	}
}

func (e *gameplaySessionRegistry) queueProjectileRetirementForOwnerLocked(req *campaignProjectileRetirement, packets [][]byte) {
	if req.arenaGameID != 0 {
		e.queueArenaProjectilePacketsLocked(req, req.sessionKey, packets)
		return
	}
	e.queueProjectileRetirementLocked(req.originalZone, packets)
}

// Contact is chosen by the ordinary moving-projectile collision sampler. Keep
// that run's effects and deletion, replacing only its speculative combat result
// with the player's committed defense/shield/resource outcome.
func (e campaignProjectileStep) commitArenaProjectileLocked(member *gameplayPeerSession, targets *arenaAbilityTargets,
	target zonenpc.Snapshot, result zoneability.BasicResult, collision sim.ProjectileBoxCollision,
) ([][]byte, error) {
	schedule := e.schedule
	packets, err := schedule.run.ResolveCollision(context.Background(), e.deadline, true, true,
		result.Damage.PreviousHealth, result.Damage.Damage, result.IsCritical && !result.Damage.IsDamageImmune,
		collision.Position, sim.Position(schedule.facing))
	if err != nil {
		return nil, fmt.Errorf("arenaProjectileResolve: %w", err)
	}
	if schedule.flight != nil {
		e.finishFlightLocked(member)
	}
	packets = filterProjectileCombatPackets(packets)
	hitPackets, err := targets.packets([]zoneability.AreaResult{{Snapshot: target, Damage: result.Damage,
		IsCritical: result.IsCritical, Definition: schedule.definition}})
	if err != nil {
		return nil, fmt.Errorf("arenaProjectileEvent: %w", err)
	}
	if schedule.runtime.logger != nil {
		schedule.runtime.logger.Printf("Arena shared projectile hit source=%d target=%d projectile=%d delay=%s damage=%g",
			schedule.sourceObjectID, target.Plan.ObjectID, schedule.projectileObjectID, e.deadline, result.Damage.Damage)
	}
	return append(packets, hitPackets...), nil
}

func abilityTargetProjectileGeometryLocked(
	runtime campaignAbilityCommandRuntime, member gameplayPeerSession, target zonenpc.Snapshot,
) (zonenpc.ProjectileGeometry, bool, error) {
	if member.binding.Mode != game.ModeArena {
		geometry, isFallback, err := campaignTargetProjectileGeometry(runtime.program, target)
		if err != nil {
			return zonenpc.ProjectileGeometry{}, false, fmt.Errorf("campaignTargetGeometry: %w", err)
		}
		return geometry, isFallback, nil
	}
	targets := arenaAbilityTargets{runtime: runtime, source: member}
	targetSessionKey, victim, isFound := targets.member(target.Plan.ObjectID)
	if !isFound || targetSessionKey == "" {
		return zonenpc.ProjectileGeometry{}, false, fmt.Errorf("arenaTargetGeometry: target %d unavailable", target.Plan.ObjectID)
	}
	physics, isPhysicsFound := runtime.program.NounPhysicsByID[victim.binding.Creatures[victim.deployedCreatureIndex].Noun]
	geometry, isFallback, err := campaignTargetProjectileGeometryForPhysics(runtime.program, target, physics, isPhysicsFound)
	if err != nil {
		return zonenpc.ProjectileGeometry{}, false, fmt.Errorf("arenaTargetPhysics: %w", err)
	}
	return geometry, isFallback, nil
}
