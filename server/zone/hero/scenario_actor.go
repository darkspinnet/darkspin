//go:build scenario

package hero

import "errors"

// RetireForScenario fences the old living identity without recording a death.
// Keep the actor as a revision tombstone until the fixture's validated Put
// commits its replacement; ordinary observations and mutations cannot use it.
func (e *Session) RetireForScenario(userID, peerGeneration uint64, objectID uint32) error {
	if e == nil || userID == 0 || peerGeneration == 0 || objectID == 0 {
		return errors.New("scenario hero retirement identity invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[userID]
	if !isFound || actor.PeerGeneration != peerGeneration || actor.ObjectID != objectID ||
		e.retiredActors[userID] || actor.HitPoint <= 0 {
		return errors.New("scenario hero retirement actor unavailable")
	}
	if actor.DeathRevision == ^uint64(0) {
		return errors.New("scenario hero retirement revision exhausted")
	}
	actor.DeathRevision++
	if e.retiredActors == nil {
		e.retiredActors = make(map[uint64]bool)
	}
	e.actors[userID] = actor
	e.retiredActors[userID] = true
	return nil
}
