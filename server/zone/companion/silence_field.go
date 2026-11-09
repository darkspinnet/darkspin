package companion

import (
	"errors"
	"maps"
	"time"
)

// The native class getter returns zero for logical78/80 and MirrorBaseAttributes
// excludes both. Own runtime grants are separate from that known baseline.
func (e *Session) SilenceImmunity(objectID uint32) (float32, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, isFound := e.actors[objectID]
	if !isFound {
		return 0, false
	}
	return actor.silenceImmunity, true
}

func (e *Session) AddSilenceLease(objectID uint32, instanceID uint32) error {
	if e == nil || objectID == 0 || instanceID == 0 {
		return errors.New("invalid companion silence lease")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || actor.HitPoint <= 0 {
		return errors.New("companion silence lease target unavailable")
	}
	actor.silenceLeases = maps.Clone(actor.silenceLeases)
	if actor.silenceLeases == nil {
		actor.silenceLeases = make(map[uint32]struct{})
	}
	actor.silenceLeases[instanceID] = struct{}{}
	e.storeCinematicActor(actor, true)
	return nil
}

func (e *Session) RemoveSilenceLease(objectID uint32, instanceID uint32) {
	if e == nil || objectID == 0 || instanceID == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound, isLive := e.cinematicActor(objectID)
	if isFound {
		actor.silenceLeases = maps.Clone(actor.silenceLeases)
		delete(actor.silenceLeases, instanceID)
		e.storeCinematicActor(actor, isLive)
	}
}

func (e Actor) isSilenced(at time.Time) bool {
	return len(e.silenceLeases) != 0 || at.Before(e.silenceEnd())
}

func (e *Session) IsSilenced(objectID uint32, at time.Time) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, isFound := e.actors[objectID]
	return isFound && actor.isSilenced(at)
}
