package zone

// AbilityCount returns the current ability boundary for an active zone member.
func (e *Zone) AbilityCount(member Member) (uint32, bool) {
	if e == nil || member.UserID == 0 || member.PeerGeneration == 0 {
		return 0, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	current, isFound := e.members[member.UserID]
	if !isFound || current.PeerGeneration != member.PeerGeneration {
		return 0, false
	}
	unlockSession, isFound := e.unlocks[member.UserID]
	if !isFound {
		return 0, false
	}
	return unlockSession.AbilityCount(), true
}

// CompareAndSetAbilityCount advances a member only from the expected boundary.
// This prevents a delayed encounter callback from overwriting newer progress.
func (e *Zone) CompareAndSetAbilityCount(
	member Member, previous uint32, next uint32,
) bool {
	if e == nil || member.UserID == 0 || member.PeerGeneration == 0 {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	current, isFound := e.members[member.UserID]
	if !isFound || current.PeerGeneration != member.PeerGeneration {
		return false
	}
	unlockSession, isFound := e.unlocks[member.UserID]
	if !isFound {
		return false
	}
	return unlockSession.CompareAndSet(previous, next)
}

// RaiseAbilityCount advances an active member without reducing newer progress.
func (e *Zone) RaiseAbilityCount(member Member, abilityCount uint32) bool {
	if e == nil || member.UserID == 0 || member.PeerGeneration == 0 {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	current, isFound := e.members[member.UserID]
	if !isFound || current.PeerGeneration != member.PeerGeneration {
		return false
	}
	unlockSession, isFound := e.unlocks[member.UserID]
	if !isFound {
		return false
	}
	return unlockSession.Raise(abilityCount)
}
