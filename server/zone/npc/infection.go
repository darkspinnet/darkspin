package npc

import "time"

// AdmitInfection retains active disease independently of a player's session.
// The DOT template excludes both disease GUIDs and immunity from spread.
func (e *Session) AdmitInfection(objectID, instanceID uint32, isPrimary bool, at time.Time) bool {
	if e == nil || objectID == 0 || instanceID == 0 || at.IsZero() {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, isFound := e.npcs[objectID]; !isFound {
		return false
	}
	if !isPrimary && (at.Before(e.infectionImmunities[objectID]) || len(e.infectionInstances[objectID]) != 0) {
		return false
	}
	if e.infectionInstances == nil {
		e.infectionInstances = make(map[uint32]map[uint32]bool)
	}
	if e.infectionInstances[objectID] == nil {
		e.infectionInstances[objectID] = make(map[uint32]bool)
	}
	e.infectionInstances[objectID][instanceID] = isPrimary
	return true
}

// RetireInfection atomically replaces an active disease with seven-second
// immunity. Primary Deactivate refreshes immunity; spread preserves it.
func (e *Session) RetireInfection(objectID, instanceID uint32, isPrimary bool, at time.Time) {
	if e == nil || objectID == 0 || instanceID == 0 || at.IsZero() {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	instances := e.infectionInstances[objectID]
	if _, isFound := instances[instanceID]; !isFound {
		return
	}
	delete(instances, instanceID)
	if len(instances) == 0 {
		delete(e.infectionInstances, objectID)
	}
	if !isPrimary && at.Before(e.infectionImmunities[objectID]) {
		return
	}
	if e.infectionImmunities == nil {
		e.infectionImmunities = make(map[uint32]time.Time)
	}
	e.infectionImmunities[objectID] = at.Add(7 * time.Second)
}

func (e *Session) IsInfectionSpreadBlocked(objectID uint32, at time.Time) bool {
	if e == nil || objectID == 0 {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.infectionInstances[objectID]) != 0 || at.Before(e.infectionImmunities[objectID])
}

// RollbackInfection undoes admission that was never accepted or published.
func (e *Session) RollbackInfection(objectID, instanceID uint32) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	instances := e.infectionInstances[objectID]
	delete(instances, instanceID)
	if len(instances) == 0 {
		delete(e.infectionInstances, objectID)
	}
}

func (e *Session) IsInfectionImmune(objectID uint32, at time.Time) bool {
	if e == nil || objectID == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	expiresAt, isFound := e.infectionImmunities[objectID]
	if !isFound {
		return false
	}
	if !at.Before(expiresAt) {
		delete(e.infectionImmunities, objectID)
		return false
	}
	return true
}

// AcceptPlague replaces each Unique GUID independently after scheduling succeeds.
// Keeping instance identities here also protects against detached-owner cleanup.
func (e *Session) AcceptPlague(objectID, instanceID, rootInstanceID uint32, rootExpiresAt, at time.Time) (uint32, uint32, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isFound := e.npcs[objectID]
	if !isFound || npc.IsDefeated || npc.HitPoint <= 0 {
		return 0, 0, false
	}
	var replacedID uint32
	for previousID, isPrimary := range e.infectionInstances[objectID] {
		if !isPrimary {
			continue
		}
		replacedID = previousID
		delete(e.infectionInstances[objectID], previousID)
		if e.infectionImmunities == nil {
			e.infectionImmunities = make(map[uint32]time.Time)
		}
		e.infectionImmunities[objectID] = at.Add(7 * time.Second)
	}
	if e.infectionInstances == nil {
		e.infectionInstances = make(map[uint32]map[uint32]bool)
	}
	if e.infectionInstances[objectID] == nil {
		e.infectionInstances[objectID] = make(map[uint32]bool)
	}
	e.infectionInstances[objectID][instanceID] = true
	replacedRootID := npc.status.bioEntangleInstanceID
	npc.status.bioEntangleInstanceID = rootInstanceID
	npc.status.bioEntangleExpiresAt = rootExpiresAt
	if isStatusBlockedByShield(npc) {
		npc.status.bioEntangleExpiresAt = time.Time{}
	}
	e.npcs[objectID] = npc
	return replacedID, replacedRootID, true
}

func (e *Session) PlagueInstances(objectID uint32) (uint32, uint32) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var instanceID uint32
	for candidateID, isPrimary := range e.infectionInstances[objectID] {
		if isPrimary {
			instanceID = candidateID
		}
	}
	return instanceID, e.npcs[objectID].status.bioEntangleInstanceID
}

func (e *Session) ClearBioEntangle(objectID, instanceID uint32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isFound := e.npcs[objectID]
	if !isFound || instanceID == 0 || npc.status.bioEntangleInstanceID != instanceID {
		return
	}
	npc.status.bioEntangleInstanceID = 0
	npc.status.bioEntangleExpiresAt = time.Time{}
	e.npcs[objectID] = npc
}
