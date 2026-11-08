package npc

import (
	"errors"
	"fmt"
	"maps"
	"time"
)

// ApplyAuraSilence reports this request's admission independently of silence
// already supplied by another modifier. Authored admission and commit observe
// the same locked actor.
func (e *Session) ApplyAuraSilence(objectID, instanceID uint32, expiresAt time.Time, admission AuraAdmission) (bool, error) {
	if e == nil || objectID == 0 || instanceID == 0 || expiresAt.IsZero() || admission == nil {
		return false, errors.New("invalid aura silence")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || actor.IsDefeated {
		return false, fmt.Errorf("auraSilenceMissing: %d", objectID)
	}
	if _, isTracked := actor.status.auraSilences[instanceID]; isTracked {
		return false, errors.New("duplicate aura silence")
	}
	isAccepted, err := admission.CanUseAura(auraImmunitySnapshot(actor))
	if err != nil {
		return false, fmt.Errorf("silenceCanUse: %w", err)
	}
	if !isAccepted {
		return false, nil
	}
	// This existing compatibility gate is separate from authored immunity.
	if isStatusBlockedByShield(actor) {
		return false, nil
	}
	endDirectionalShield(&actor)
	// Snapshots may outlive the session lock. Replace this private map rather
	// than changing a contribution visible through an earlier snapshot.
	actor.status.auraSilences = maps.Clone(actor.status.auraSilences)
	if actor.status.auraSilences == nil {
		actor.status.auraSilences = make(map[uint32]time.Time)
	}
	actor.status.auraSilences[instanceID] = expiresAt
	e.npcs[objectID] = actor
	return true, nil
}

// ClearAuraSilence removes only the contribution owned by this exact handle.
// A missing contribution is expected after dispel or corpse status cleanup.
func (e *Session) ClearAuraSilence(objectID, instanceID uint32) {
	if e == nil || objectID == 0 || instanceID == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound {
		return
	}
	if _, isTracked := actor.status.auraSilences[instanceID]; !isTracked {
		return
	}
	actor.status.auraSilences = maps.Clone(actor.status.auraSilences)
	delete(actor.status.auraSilences, instanceID)
	e.npcs[objectID] = actor
}

func (e status) silenceEnd() time.Time {
	expiresAt := e.silenceExpiresAt
	for _, contributionEnd := range e.auraSilences {
		if expiresAt.Before(contributionEnd) {
			expiresAt = contributionEnd
		}
	}
	return expiresAt
}
