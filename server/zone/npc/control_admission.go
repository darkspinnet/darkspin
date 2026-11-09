package npc

import (
	"errors"
	"fmt"
	"time"
)

// AdmitStun reports this request's acceptance rather than an existing stun,
// banish or sleep contributing to StunRemaining.
func (e *Session) AdmitStun(objectID uint32, at, expiresAt time.Time) (bool, error) {
	isAdmitted, err := e.admitControlStatus(objectID, at, expiresAt, "stun")
	if err != nil {
		return false, fmt.Errorf("stunAdmission: %w", err)
	}
	return isAdmitted, nil
}

// AdmitSilence reports this request's acceptance independently of existing
// silence. The established shield and own-immunity policies remain unchanged.
func (e *Session) AdmitSilence(objectID uint32, at, expiresAt time.Time) (bool, error) {
	isAdmitted, err := e.admitControlStatus(objectID, at, expiresAt, "silence")
	if err != nil {
		return false, fmt.Errorf("silenceAdmission: %w", err)
	}
	return isAdmitted, nil
}

func (e *Session) admitControlStatus(objectID uint32, at, expiresAt time.Time, kind string) (bool, error) {
	if e == nil || objectID == 0 || at.IsZero() || expiresAt.IsZero() {
		return false, errors.New("invalid control request")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || actor.IsDefeated {
		return false, fmt.Errorf("controlMissing: %d", objectID)
	}
	immunity := actor.ControlImmunity(at)
	if isStatusBlockedByShield(actor) || kind == "stun" && immunity.Stun > 0 || kind == "silence" && immunity.Silence > 0 {
		return false, nil
	}
	switch kind {
	case "stun":
		if actor.status.stunExpiresAt.Before(expiresAt) {
			actor.status.stunExpiresAt = expiresAt
		}
	case "silence":
		if actor.status.silenceExpiresAt.Before(expiresAt) {
			actor.status.silenceExpiresAt = expiresAt
		}
	default:
		return false, errors.New("unsupported control request")
	}
	endDirectionalShield(&actor)
	e.npcs[objectID] = actor
	return true, nil
}
