package npc

import "fmt"

// InitializeHauntStealth runs once for the world actor, independently of a
// transport reconnection. A revealed Haunt must stay revealed after rejoin.
func (e *Session) InitializeHauntStealth(objectID uint32) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || actor.IsDefeated {
		return false, fmt.Errorf("hauntInitializeMissing: %d", objectID)
	}
	if actor.status.isHauntInitialized {
		return false, nil
	}
	actor.status.isHauntInitialized = true
	actor.status.isStealthed = true
	profile, isProfileFound := ActionProfileForPlan(actor.Plan)
	if !isProfileFound {
		return false, fmt.Errorf("hauntInitializeProfile: %d", objectID)
	}
	profile.StealthType = 2
	actor.Plan.ActionProfile = profile
	actor.Plan.IsActionKnown = true
	e.npcs[objectID] = actor
	return true, nil
}

func (e *Session) RevealHaunt(objectID uint32) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound {
		return fmt.Errorf("hauntRevealMissing: %d", objectID)
	}
	actor.status.isStealthed = false
	actor.status.isHauntInitialized = true
	profile, isProfileFound := ActionProfileForPlan(actor.Plan)
	if !isProfileFound {
		return fmt.Errorf("hauntRevealProfile: %d", objectID)
	}
	profile.StealthType = 0
	actor.Plan.ActionProfile = profile
	actor.Plan.IsActionKnown = true
	e.npcs[objectID] = actor
	return nil
}
