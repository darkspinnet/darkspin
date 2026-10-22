package companion

import (
	"errors"
	"time"
)

// Resurrection preserves the pet-private allowance and ability ownership from
// BeastPetPassive. The ordinary allowance is owned by the player's squad.
type Resurrection struct {
	IsOverdriveUsed bool
	IsCasting       bool
	IsBurrowing     bool
	HitAt           time.Time
	ReleaseAt       time.Time
	IsHitApplied    bool
}

func (e *Resurrection) IsEligible(isOverdrive bool, isNormalAvailable bool) bool {
	return e != nil && ((isOverdrive && !e.IsOverdriveUsed) || isNormalAvailable)
}

// Begin consumes the OD bit even before the hit, matching the authored request
// branch. During OD the player allowance is never consumed by that branch.
func (e *Resurrection) Begin(at time.Time, isOverdrive bool) bool {
	if e == nil || e.IsCasting || e.IsBurrowing || at.IsZero() {
		return false
	}
	e.IsCasting = true
	e.IsHitApplied = false
	e.HitAt = at.Add(time.Second)
	e.ReleaseAt = at.Add(1400 * time.Millisecond)
	if isOverdrive {
		e.IsOverdriveUsed = true
	}
	return true
}

func (e *Resurrection) ObserveOverdrive(isOverdrive bool) {
	if e != nil && !isOverdrive {
		e.IsOverdriveUsed = false
	}
}

// ControlAbility cancels prior movement reservations at their sampled pose.
// Both resurrection and burrow own locomotion and make the pet intangible.
func (e *Session) ControlAbility(objectID uint32, at time.Time, until time.Time, isControlled bool, animation string) error {
	if e == nil || at.IsZero() {
		return errors.New("companion control unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || actor.HitPoint <= 0 {
		return errors.New("companion control actor unavailable")
	}
	actor.Position = actor.followPosition(at)
	actor.clearFollow()
	actor.FollowRevision = e.nextFollowRevision()
	actor.clearPursuit()
	actor.TargetObjectID = 0
	actor.IsAbilityControlled = isControlled
	actor.IsIntangible = isControlled
	actor.AbilityAnimation = animation
	actor.followHoldUntil = until
	e.actors[objectID] = actor
	return nil
}
