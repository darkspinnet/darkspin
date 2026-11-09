package companion

import (
	"errors"
	"maps"
	"time"
)

// ApplySilence blocks new ability admissions without retracting attacks that
// have already launched. A refreshed child keeps the longest active deadline.
func (e *Session) ApplySilence(objectID uint32, expiresAt time.Time) error {
	if e == nil || objectID == 0 || expiresAt.IsZero() {
		return errors.New("companion silence invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || actor.HitPoint <= 0 {
		return errors.New("companion silence target unavailable")
	}
	if expiresAt.After(actor.silenceExpiresAt) {
		actor.silenceExpiresAt = expiresAt
	}
	e.storeCinematicActor(actor, true)
	return nil
}

func (e *Session) ClearSilence(objectID uint32, expiresAt time.Time) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound, isLive := e.cinematicActor(objectID)
	if isFound && actor.silenceExpiresAt == expiresAt {
		actor.silenceExpiresAt = time.Time{}
		e.storeCinematicActor(actor, isLive)
	}
}

// ApplySilenceModifier replaces only this full handle's finite contribution.
// New admissions remain blocked while any independent silence is active.
func (e *Session) ApplySilenceModifier(objectID, instanceID uint32, expiresAt time.Time) error {
	if e == nil || objectID == 0 || instanceID == 0 || expiresAt.IsZero() {
		return errors.New("invalid companion silence modifier")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || actor.HitPoint <= 0 {
		return errors.New("companion silence modifier target unavailable")
	}
	actor.silenceModifiers = maps.Clone(actor.silenceModifiers)
	if actor.silenceModifiers == nil {
		actor.silenceModifiers = make(map[uint32]time.Time)
	}
	actor.silenceModifiers[instanceID] = expiresAt
	e.storeCinematicActor(actor, true)
	return nil
}

// ClearSilenceModifier cannot remove another handle with the same deadline.
func (e *Session) ClearSilenceModifier(objectID, instanceID uint32) {
	if e == nil || objectID == 0 || instanceID == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound, isLive := e.cinematicActor(objectID)
	if !isFound {
		return
	}
	if _, isTracked := actor.silenceModifiers[instanceID]; !isTracked {
		return
	}
	actor.silenceModifiers = maps.Clone(actor.silenceModifiers)
	delete(actor.silenceModifiers, instanceID)
	e.storeCinematicActor(actor, isLive)
}

func (e Actor) silenceEnd() time.Time {
	expiresAt := e.silenceExpiresAt
	for _, contributionEnd := range e.silenceModifiers {
		if expiresAt.Before(contributionEnd) {
			expiresAt = contributionEnd
		}
	}
	return expiresAt
}

func (e *Actor) clearSilence() {
	e.silenceExpiresAt = time.Time{}
	e.silenceModifiers = nil
	e.silenceLeases = nil
}

func (e *Session) SilenceRemaining(objectID uint32, at time.Time) time.Duration {
	if e == nil {
		return 0
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, isFound := e.actors[objectID]
	if !isFound {
		return 0
	}
	return max(time.Duration(0), actor.silenceEnd().Sub(at))
}
