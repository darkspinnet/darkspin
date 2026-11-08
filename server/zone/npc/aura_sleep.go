package npc

import (
	"errors"
	"fmt"
	"maps"
	"time"
)

// ApplyAuraSleep reports this cloud's actual admission and records only its
// exact handle. Existing sleep from another source cannot turn rejection into
// acceptance. Authored admission and commit observe the same locked actor.
func (e *Session) ApplyAuraSleep(objectID, instanceID uint32, at, expiresAt time.Time, admission AuraAdmission) (bool, error) {
	if e == nil || objectID == 0 || instanceID == 0 || at.IsZero() || expiresAt.IsZero() || admission == nil {
		return false, errors.New("invalid aura sleep")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || actor.IsDefeated {
		return false, fmt.Errorf("auraSleepMissing: %d", objectID)
	}
	if _, isTracked := actor.status.auraSleeps[instanceID]; isTracked {
		return false, errors.New("duplicate aura sleep")
	}
	isAccepted, err := admission.CanUseAura(auraImmunitySnapshot(actor, at))
	if err != nil {
		return false, fmt.Errorf("sleepCanUse: %w", err)
	}
	if !isAccepted {
		return false, nil
	}
	// This existing compatibility gate is separate from authored immunity.
	if isStatusBlockedByShield(actor) {
		return false, nil
	}
	// Earlier snapshots may still be observed outside the session lock.
	actor.status.auraSleeps = maps.Clone(actor.status.auraSleeps)
	if actor.status.auraSleeps == nil {
		actor.status.auraSleeps = make(map[uint32]time.Time)
	}
	actor.status.auraSleeps[instanceID] = expiresAt
	e.npcs[objectID] = actor
	return true, nil
}

// ClearAuraSleep removes only this cloud's contribution. The caller that owns
// a direct-damage wake removes each affected handle after queuing its deletion.
// Missing contributions are expected after dispel or corpse cleanup.
func (e *Session) ClearAuraSleep(objectID, instanceID uint32) {
	if e == nil || objectID == 0 || instanceID == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound {
		return
	}
	if _, isTracked := actor.status.auraSleeps[instanceID]; !isTracked {
		return
	}
	actor.status.auraSleeps = maps.Clone(actor.status.auraSleeps)
	delete(actor.status.auraSleeps, instanceID)
	e.npcs[objectID] = actor
}

func (e status) sleepEnd() time.Time {
	expiresAt := e.sleepExpiresAt
	for _, contributionEnd := range e.auraSleeps {
		if expiresAt.Before(contributionEnd) {
			expiresAt = contributionEnd
		}
	}
	return expiresAt
}
