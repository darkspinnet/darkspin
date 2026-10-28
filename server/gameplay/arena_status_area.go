package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

// Dimensional Rift applies build-103 BanishedModifier to each opposing hero
// inside its cursor area. The recipient owns the duration and removal.
func (e heroStatusAreaSchedule) applyArenaBanishLocked(source gameplayPeerSession) ([][]byte, error) {
	defer e.runtime.registry.mutex.Unlock()
	match := e.runtime.registry.arenaMatches[source.binding.GameID]
	if match == nil || !match.isCombatActive || source.deployedHitPoint() <= 0 ||
		source.deployedObjectID != e.sourceObjectID || e.definition.StatusKind != sim.AbilityStatusKindBanish {
		return nil, nil
	}
	timestamp := e.packet.SourceTime + uint64(e.definition.HitDelay/time.Millisecond)
	effectPacket, err := raknet.MarshalApplication(raknet.DropPresentationMessage{
		Asset: util.HashID(e.definition.ActivationEffectName), Position: e.center,
	})
	if err != nil {
		return nil, fmt.Errorf("arenaBanishEffect: %w", err)
	}
	e.runtime.registry.queueFieldMedicRealmPacketsLocked(nil, source.binding.GameID, [][]byte{effectPacket})
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, source, e.definition, timestamp).(*arenaAbilityTargets)
	objectIDs := make([]uint32, 0)
	for _, target := range targets.LiveSnapshots() {
		if zonegeometry.Distance(game.Vec3(e.center), target.Plan.Position) <= e.definition.Radius {
			objectIDs = append(objectIDs, target.Plan.ObjectID)
		}
	}
	plan := zoneability.AreaPlan{SourceObjectID: e.sourceObjectID, AbilityID: e.abilityID,
		Definition: e.definition, Center: game.Vec3(e.center)}
	_, err = e.runtime.applyArenaTargetStatusLocked(e.sessionKey, e.generation,
		e.sourceObjectID, timestamp, plan, objectIDs)
	if err != nil {
		return nil, fmt.Errorf("arenaBanishStatus: %w", err)
	}
	return nil, nil
}

// Called with the registry locked by the shared status-area schedule. Curse
// lives on each Arena recipient, so a caster switch cannot erase its profile.
func (e heroStatusAreaSchedule) applyArenaVoodooLocked(source gameplayPeerSession) ([][]byte, error) {
	defer e.runtime.registry.mutex.Unlock()
	match := e.runtime.registry.arenaMatches[source.binding.GameID]
	if match == nil || !match.isCombatActive || source.deployedHitPoint() <= 0 ||
		source.deployedObjectID != e.sourceObjectID {
		return nil, nil
	}
	timestamp := e.packet.SourceTime + uint64(e.definition.HitDelay/time.Millisecond)
	effectPacket, err := raknet.MarshalApplication(raknet.DropPresentationMessage{
		Asset: util.HashID(e.definition.ActivationEffectName), Position: e.center,
	})
	if err != nil {
		return nil, fmt.Errorf("arenaCurseEffect: %w", err)
	}
	e.runtime.registry.queueFieldMedicRealmPacketsLocked(nil, source.binding.GameID, [][]byte{effectPacket})
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, source, e.definition, timestamp).(*arenaAbilityTargets)
	objectIDs := make([]uint32, 0)
	for _, target := range targets.LiveSnapshots() {
		if zonegeometry.Distance(game.Vec3(e.center), target.Plan.Position) <= e.definition.Radius {
			objectIDs = append(objectIDs, target.Plan.ObjectID)
		}
	}
	plan := zoneability.AreaPlan{SourceObjectID: e.sourceObjectID, AbilityID: e.abilityID,
		Definition: e.definition, Center: game.Vec3(e.center)}
	_, err = e.runtime.applyArenaTargetStatusLocked(e.sessionKey, e.generation,
		e.sourceObjectID, timestamp, plan, objectIDs)
	if err != nil {
		return nil, fmt.Errorf("arenaCurseStatus: %w", err)
	}
	return nil, nil
}
