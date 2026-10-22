package companion

import (
	"errors"
	"time"
)

// Recall immobilizes a pet without the intangibility used by Beast's cast.
// LifeTempestPetBurrow only adds Immobilized while its animation finishes.
func (e *Session) Recall(objectID uint32, at time.Time, until time.Time, animation string) error {
	if e == nil || at.IsZero() || !until.After(at) || animation == "" {
		return errors.New("companion recall invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || actor.HitPoint <= 0 {
		return errors.New("companion recall actor unavailable")
	}
	actor.Position = actor.followPosition(at)
	actor.clearFollow()
	actor.FollowRevision = e.nextFollowRevision()
	actor.clearPursuit()
	actor.TargetObjectID = 0
	actor.IsAbilityControlled = true
	actor.AbilityAnimation = animation
	actor.followHoldUntil = until
	e.actors[objectID] = actor
	return nil
}

func (e *Session) ReleaseRecall(objectID uint32, animation string) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || !actor.IsAbilityControlled || actor.AbilityAnimation != animation {
		return false
	}
	actor.IsAbilityControlled = false
	actor.AbilityAnimation = ""
	actor.followHoldUntil = time.Time{}
	actor.FollowRevision = e.nextFollowRevision()
	e.actors[objectID] = actor
	return true
}

// QuiesceCombat releases connection-local reservations while preserving an
// emergence/recall phase and the pet's existing follow deadline.
func (e *Session) QuiesceCombat(objectID uint32) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound {
		return false
	}
	actor.TargetObjectID = 0
	actor.clearPursuit()
	e.actors[objectID] = actor
	return true
}
