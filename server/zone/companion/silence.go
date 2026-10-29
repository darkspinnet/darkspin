package companion

import (
	"errors"
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
	e.actors[objectID] = actor
	return nil
}

func (e *Session) ClearSilence(objectID uint32, expiresAt time.Time) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if isFound && actor.silenceExpiresAt == expiresAt {
		actor.silenceExpiresAt = time.Time{}
		e.actors[objectID] = actor
	}
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
	return max(time.Duration(0), actor.silenceExpiresAt.Sub(at))
}
