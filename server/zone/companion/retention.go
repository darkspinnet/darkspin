package companion

import "sort"

// CaptureRetainedOwner begins the one disconnect transfer. Cleanup may remove
// live summons afterward; cinematic callbacks still own this authoritative copy.
func (e *Session) CaptureRetainedOwner(userID uint64, peerGeneration uint64) []Actor {
	if e == nil || userID == 0 || peerGeneration == 0 {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.retainedActors == nil {
		e.retainedActors = make(map[uint32]Actor)
	}
	actors := make([]Actor, 0)
	for objectID, actor := range e.actors {
		if actor.UserID != userID || actor.PeerGeneration != peerGeneration {
			continue
		}
		e.retainedActors[objectID] = actor
		actors = append(actors, actor)
	}
	sort.Slice(actors, func(left int, right int) bool {
		return actors[left].ObjectID < actors[right].ObjectID
	})
	return actors
}

// Call with the session mutex held. Live state takes precedence while cleanup
// has not removed it; afterward only an explicitly captured transfer is visible.
func (e *Session) cinematicActor(objectID uint32) (Actor, bool, bool) {
	actor, isLive := e.actors[objectID]
	if isLive {
		return actor, true, true
	}
	actor, isRetained := e.retainedActors[objectID]
	return actor, isRetained, false
}

func (e *Session) storeCinematicActor(actor Actor, isLive bool) {
	if isLive {
		e.actors[actor.ObjectID] = actor
	}
	retained, isRetained := e.retainedActors[actor.ObjectID]
	if isRetained && retained.lifeRevision == actor.lifeRevision {
		e.retainedActors[actor.ObjectID] = actor
	}
}

func (e *Session) discardRetained(actor Actor) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	retained, isFound := e.retainedActors[actor.ObjectID]
	if isFound && retained.lifeRevision == actor.lifeRevision {
		delete(e.retainedActors, actor.ObjectID)
	}
}
