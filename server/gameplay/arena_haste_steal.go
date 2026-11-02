package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// Spacetime Tempest steals a live haste modifier after a damaging hit. The
// source receives the target's remaining stack lifetime, while the target's
// original instance is retired through its recipient-owned Arena status.
func (e *arenaAbilityTargets) applyArenaLightspeedStealLocked(plan zoneability.AreaPlan,
	results []zoneability.AreaResult,
) error {
	if e.runtime.registry.timer == nil {
		return nil
	}
	sourceKey := ""
	for key, member := range e.runtime.registry.sessions {
		if member.binding.Mode == game.ModeArena && member.binding.GameID == e.source.binding.GameID &&
			member.generation == e.source.generation && member.deployedObjectID == plan.SourceObjectID {
			sourceKey = key
			break
		}
	}
	source, isSourceFound := e.runtime.registry.sessions[sourceKey]
	if !isSourceFound || source.binding.Mode != game.ModeArena || source.binding.GameID != e.source.binding.GameID ||
		source.generation != e.source.generation || !source.isArenaCombatActive ||
		source.deployedObjectID != plan.SourceObjectID {
		return nil
	}
	statusKey := arenaHitStatusKey{modifierID: util.HashID("LightSpeedHaste")}
	for _, result := range results {
		if result.Damage.IsDamageImmune || result.Damage.Damage <= 0 {
			continue
		}
		targetKey, target, isTargetFound := e.member(result.Damage.ObjectID)
		if !isTargetFound || targetKey == sourceKey {
			continue
		}
		original := target.arenaHitStatuses[statusKey]
		if original == nil || !original.matches(target) || original.kind != arenaBasicHaste {
			continue
		}
		duration := original.expiresAt.Sub(e.runtime.now())
		if duration <= 0 {
			continue
		}
		previous := source.arenaHitStatuses[statusKey]
		stackCount := original.stackCount
		if previous != nil && previous.matches(source) && previous.kind == arenaBasicHaste {
			stackCount = min(uint32(5), stackCount+previous.stackCount)
			duration = max(duration, previous.expiresAt.Sub(e.runtime.now()))
		}
		status, creation, err := e.prepareBasicStatusLocked(sourceKey, source, source.deployedObjectID,
			"LightSpeedHaste", arenaBasicHaste, original.rank, stackCount, duration, e.timestamp)
		if err != nil {
			return fmt.Errorf("arenaHastePrepare: %w", err)
		}
		if source.arenaHitStatuses == nil {
			source.arenaHitStatuses = make(map[arenaHitStatusKey]*arenaHitStatus)
		}
		source.arenaHitStatuses[statusKey] = status
		attributes, err := status.refresh(&source)
		if err != nil {
			if previous == nil {
				delete(source.arenaHitStatuses, statusKey)
			} else {
				source.arenaHitStatuses[statusKey] = previous
			}
			return status.discard("arenaHasteRefresh", err)
		}
		original.retire(&target)
		e.runtime.registry.sessions[targetKey] = target
		if previous != nil {
			previous.retire(&source)
		}
		status.queue(&source, append([][]byte{creation}, attributes...))
		e.runtime.registry.sessions[sourceKey] = source
		mark, err := npcraknet.PositionedEffect("spacetime_AOE_hasteMark_effect.ServerEventDef",
			game.Vec3(target.playerPosition))
		if err != nil {
			return fmt.Errorf("arenaHasteMark: %w", err)
		}
		e.runtime.registry.queueFieldMedicRealmPacketsLocked(nil, source.binding.GameID, [][]byte{mark})
	}
	return nil
}
