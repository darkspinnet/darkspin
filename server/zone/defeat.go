package zone

// CanResumeDefeatedMember preserves a fallen player's place while a connected
// ally can still play the mission. A local squad wipe is not a party exit.
func (e *Registry) CanResumeDefeatedMember(gameID uint32, userID uint64) bool {
	current, isFound := e.Get(uint64(gameID))
	if !isFound || (current.Boss() != nil && current.Boss().IsBeamOutCommitted()) {
		return false
	}
	current.mu.RLock()
	defer current.mu.RUnlock()
	fallenSquad, isFound := current.checkpointSquads[userID]
	if !isFound || !fallenSquad.State.IsGameOver {
		return false
	}
	for allyID, ally := range current.members {
		if allyID == userID || !ally.IsConnected {
			continue
		}
		allySquad, isFound := current.checkpointSquads[allyID]
		if isFound && !allySquad.State.IsGameOver {
			return true
		}
	}
	return false
}

// IsPartyDefeated checks complete squads, including reserve heroes. A member
// whose deployment has not supplied a squad yet must not cause an early wipe.
func (e *Zone) IsPartyDefeated() bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	isMemberFound := false
	for userID, member := range e.members {
		if !member.IsConnected {
			continue
		}
		isMemberFound = true
		memberSquad, isFound := e.checkpointSquads[userID]
		if !isFound || !memberSquad.State.IsGameOver {
			return false
		}
	}
	return isMemberFound
}
