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
		e.infectionInstances = make(map[uint32]map[uint32]struct{})
	}
	if e.infectionInstances[objectID] == nil {
		e.infectionInstances[objectID] = make(map[uint32]struct{})
	}
	e.infectionInstances[objectID][instanceID] = struct{}{}
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
