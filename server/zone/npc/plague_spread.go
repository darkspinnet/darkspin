package npc

import (
	"sort"
	"time"
)

func (e *Session) InfectionSpreadInstances(objectID uint32) []uint32 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	instanceIDs := make([]uint32, 0)
	for instanceID, isPrimary := range e.infectionInstances[objectID] {
		if !isPrimary {
			instanceIDs = append(instanceIDs, instanceID)
		}
	}
	sort.Slice(instanceIDs, func(left, right int) bool { return instanceIDs[left] < instanceIDs[right] })
	return instanceIDs
}

// Direct authored requests are Unique replacements. Contagious spread uses
// AdmitInfection instead, which excludes immunity and either disease GUID.
func (e *Session) AcceptInfectionSpread(objectID, instanceID uint32, at time.Time) ([]uint32, bool) {
	if e == nil || objectID == 0 || instanceID == 0 || at.IsZero() {
		return nil, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	target, isFound := e.npcs[objectID]
	if !isFound || target.IsDefeated || target.HitPoint <= 0 {
		return nil, false
	}
	replacedIDs := make([]uint32, 0)
	for previousID, isPrimary := range e.infectionInstances[objectID] {
		if !isPrimary {
			replacedIDs = append(replacedIDs, previousID)
			delete(e.infectionInstances[objectID], previousID)
		}
	}
	// The old spread's Deactivate preserves an already active immunity.
	if len(replacedIDs) != 0 && !at.Before(e.infectionImmunities[objectID]) {
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
	e.infectionInstances[objectID][instanceID] = false
	sort.Slice(replacedIDs, func(left, right int) bool { return replacedIDs[left] < replacedIDs[right] })
	return replacedIDs, true
}
