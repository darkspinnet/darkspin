package companion

import (
	"errors"
	"math"
)

// RestoreHitPoint accepts a prepared healing publication only while its actor
// identity and resources still match. A rejected change never mutates the pet.
func (e *Session) RestoreHitPoint(expected Actor, hitPoint float32) (bool, error) {
	if e == nil {
		return false, errors.New("nil companion session")
	}
	if expected.ObjectID == 0 || math.IsNaN(float64(hitPoint)) ||
		math.IsInf(float64(hitPoint), 0) || hitPoint < expected.HitPoint ||
		hitPoint > expected.MaximumHitPoint {
		return false, errors.New("companion recovery invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[expected.ObjectID]
	if !isFound || actor.UserID != expected.UserID ||
		actor.PeerGeneration != expected.PeerGeneration ||
		actor.OwnerObjectID != expected.OwnerObjectID || actor.Noun != expected.Noun ||
		actor.DeathRevision != expected.DeathRevision ||
		actor.HitPoint != expected.HitPoint || actor.MaximumHitPoint != expected.MaximumHitPoint ||
		!actor.IsCombatant || !actor.IsTargetable || actor.HitPoint <= 0 {
		return false, nil
	}
	actor.HitPoint = hitPoint
	e.storeCinematicActor(actor, true)
	return true, nil
}
