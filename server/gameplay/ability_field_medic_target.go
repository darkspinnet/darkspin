package gameplay

import (
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type fieldMedicHealingTarget struct {
	sessionKey    string
	generation    uint64
	userID        uint64
	objectID      uint32
	creatureIndex uint32
	binding       game.GameplayBinding
	position      game.Vec3
	isCompanion   bool
}

func fieldMedicSupportTargetLocked(
	registry *gameplaySessionRegistry, sourceSessionKey string,
	sourceSession gameplayPeerSession,
	requestedObjectID uint32, cursor game.Vec3, radius float32,
) fieldMedicHealingTarget {
	fallback := fieldMedicHealingTarget{
		sessionKey: sourceSessionKey,
		generation: sourceSession.generation, userID: sourceSession.binding.UserID,
		objectID:      sourceSession.deployedObjectID,
		creatureIndex: sourceSession.deployedCreatureIndex,
		binding:       sourceSession.binding, position: game.Vec3(sourceSession.playerPosition),
	}
	if registry == nil || sourceSession.zone == nil || radius <= 0 {
		return fallback
	}
	nearestDistance := float32(math.MaxFloat32)
	selected := fieldMedicHealingTarget{}
	for sessionKey, candidate := range registry.sessions {
		if candidate.zone != sourceSession.zone || candidate.squad == nil ||
			candidate.deployedObjectID == 0 ||
			candidate.deployedHitPoint() <= 0 ||
			candidate.deployedCreatureIndex >= uint32(len(candidate.binding.Creatures)) {
			continue
		}
		distanceFromSource := zonegeometry.Distance(
			game.Vec3(sourceSession.playerPosition), game.Vec3(candidate.playerPosition),
		)
		if distanceFromSource > radius {
			continue
		}
		current := fieldMedicHealingTarget{
			sessionKey: sessionKey, generation: candidate.generation,
			userID: candidate.binding.UserID, objectID: candidate.deployedObjectID,
			creatureIndex: candidate.deployedCreatureIndex, binding: candidate.binding,
			position: game.Vec3(candidate.playerPosition),
		}
		if requestedObjectID != 0 && candidate.deployedObjectID == requestedObjectID {
			return current
		}
		distanceFromCursor := zonegeometry.Distance(
			cursor, game.Vec3(candidate.playerPosition),
		)
		if distanceFromCursor > radius || distanceFromCursor >= nearestDistance {
			continue
		}
		nearestDistance = distanceFromCursor
		selected = current
	}
	for _, companion := range sourceSession.zone.Companion().Snapshots() {
		if !companion.IsTargetable || companion.HitPoint <= 0 {
			continue
		}
		distanceFromSource := zonegeometry.Distance(
			game.Vec3(sourceSession.playerPosition), companion.Position,
		)
		if distanceFromSource > radius {
			continue
		}
		ownerSessionKey := ""
		ownerSession := gameplayPeerSession{}
		for sessionKey, candidate := range registry.sessions {
			if candidate.zone == sourceSession.zone &&
				candidate.binding.UserID == companion.UserID &&
				candidate.generation == companion.PeerGeneration {
				ownerSessionKey = sessionKey
				ownerSession = candidate
				break
			}
		}
		if ownerSessionKey == "" {
			continue
		}
		current := fieldMedicHealingTarget{
			sessionKey: ownerSessionKey, generation: companion.PeerGeneration,
			userID: companion.UserID, objectID: companion.ObjectID,
			binding: ownerSession.binding, position: companion.Position,
			isCompanion: true,
		}
		if requestedObjectID != 0 && companion.ObjectID == requestedObjectID {
			return current
		}
		distanceFromCursor := zonegeometry.Distance(cursor, companion.Position)
		if distanceFromCursor > radius || distanceFromCursor >= nearestDistance {
			continue
		}
		nearestDistance = distanceFromCursor
		selected = current
	}
	if selected.objectID != 0 {
		return selected
	}
	return fallback
}
