package gameplay

import zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"

// A fallen deployed hero may still own living squad slots or companions.
// Participation is separate from each ability's liveness and range rules.
func isActivePartyRecipient(candidate, source gameplayPeerSession) bool {
	if source.zone == nil || candidate.zone != source.zone ||
		candidate.binding.GameID != source.binding.GameID ||
		candidate.binding.Team != source.binding.Team || candidate.squad == nil ||
		!candidate.stage.IsDungeon() || !candidate.dungeonSetup.IsCommitted() ||
		candidate.isRejoinPending || candidate.isZoneTerminal() ||
		candidate.lastPlayerStatus.Status&0x40 != 0 ||
		candidate.deployedObjectID == 0 ||
		candidate.deployedCreatureIndex >= uint32(len(candidate.binding.Creatures)) {
		return false
	}
	isConnected := false
	for _, member := range candidate.zone.Snapshot().Members {
		if member.UserID == candidate.binding.UserID &&
			member.PeerGeneration == candidate.generation {
			isConnected = member.IsConnected
			break
		}
	}
	if !isConnected {
		return false
	}
	actor, isActorFound := candidate.zone.Hero().Snapshot(
		candidate.binding.UserID, candidate.generation,
	)
	return isActorFound && actor.ObjectID == candidate.deployedObjectID &&
		actor.CreatureIndex == candidate.deployedCreatureIndex
}

func isPartyCompanionOwner(actor zonecompanion.Actor, candidate gameplayPeerSession) bool {
	return actor.UserID == candidate.binding.UserID &&
		actor.PeerGeneration == candidate.generation &&
		actor.OwnerObjectID == candidate.deployedObjectID
}

// Called with the registry locked, including delayed healing applications.
func (e *gameplaySessionRegistry) isActivePartyCompanionLocked(
	actor zonecompanion.Actor, source gameplayPeerSession,
) bool {
	for _, candidate := range e.sessions {
		if isPartyCompanionOwner(actor, candidate) &&
			isActivePartyRecipient(candidate, source) {
			return true
		}
	}
	return false
}
