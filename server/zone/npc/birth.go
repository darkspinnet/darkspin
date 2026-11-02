package npc

import "fmt"

// Baseline materialization retains accepted births even while their live wire
// publication is pending. Combat selection uses LiveSnapshots instead.
func (e *Session) LivePresentationSnapshots() []Snapshot {
	return e.liveSnapshots(true)
}

// AddPendingBirth retains complete snapshots for catchup, while preventing any
// world poll from acquiring an action before the birth publication commits.
func (e *Session) AddPendingBirth(plans []SpawnPlan, targetObjectID uint32) error {
	err := e.CanAdd(plans, targetObjectID)
	if err != nil {
		return fmt.Errorf("birthValidate: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	err = e.add(plans, targetObjectID)
	if err != nil {
		return fmt.Errorf("birthAdd: %w", err)
	}
	for _, plan := range plans {
		actor := e.npcs[plan.ObjectID]
		actor.IsBirthPending = true
		e.npcs[plan.ObjectID] = actor
	}
	return nil
}

// The original carrier reserves initial action admission when releasing the
// birth gate. Ordinary target polling cannot steal the brief admission window.
func (e *Session) CommitBirth(plans []SpawnPlan, owner ActionOwner) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, plan := range plans {
		actor, isFound := e.npcs[plan.ObjectID]
		if !isFound || actor.IsDefeated || !actor.IsBirthPending {
			continue
		}
		actor.IsBirthPending = false
		actor.birthOwner = owner
		e.npcs[plan.ObjectID] = actor
	}
}

func (e *Session) ReleaseBirthOwner(plans []SpawnPlan, owner ActionOwner) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, plan := range plans {
		actor, isFound := e.npcs[plan.ObjectID]
		if isFound && actor.birthOwner == owner {
			actor.birthOwner = ActionOwner{}
			e.npcs[plan.ObjectID] = actor
		}
	}
}
