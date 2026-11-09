package npc

import (
	"errors"
	"fmt"
	"maps"
	"time"
)

// Modifier controls retain each admitted native instance independently. Legacy
// expiry-token controls remain separate contributions for callers without a
// modifier handle.
func (e *Session) AdmitStunModifier(objectID, instanceID uint32, at, expiresAt time.Time) (bool, error) {
	isAdmitted, err := e.admitControlModifier(objectID, instanceID, at, expiresAt, "stun")
	if err != nil {
		return false, fmt.Errorf("stunModifier: %w", err)
	}
	return isAdmitted, nil
}

func (e *Session) AdmitSilenceModifier(objectID, instanceID uint32, at, expiresAt time.Time) (bool, error) {
	isAdmitted, err := e.admitControlModifier(objectID, instanceID, at, expiresAt, "silence")
	if err != nil {
		return false, fmt.Errorf("silenceModifier: %w", err)
	}
	return isAdmitted, nil
}

func (e *Session) AdmitBanishModifier(objectID, instanceID uint32, at, expiresAt time.Time) (bool, error) {
	isAdmitted, err := e.admitControlModifier(objectID, instanceID, at, expiresAt, "banish")
	if err != nil {
		return false, fmt.Errorf("banishModifier: %w", err)
	}
	return isAdmitted, nil
}

func (e *Session) AdmitSleepModifier(objectID, instanceID uint32, at, expiresAt time.Time) (bool, error) {
	isAdmitted, err := e.admitControlModifier(objectID, instanceID, at, expiresAt, "sleep")
	if err != nil {
		return false, fmt.Errorf("sleepModifier: %w", err)
	}
	return isAdmitted, nil
}

func (e *Session) AdmitRootModifier(objectID, instanceID uint32, at, expiresAt time.Time) (bool, error) {
	isAdmitted, err := e.admitControlModifier(objectID, instanceID, at, expiresAt, "root")
	if err != nil {
		return false, fmt.Errorf("rootModifier: %w", err)
	}
	return isAdmitted, nil
}

func (e *Session) admitControlModifier(objectID, instanceID uint32, at, expiresAt time.Time, kind string) (bool, error) {
	if e == nil || objectID == 0 || instanceID == 0 || at.IsZero() || !expiresAt.After(at) {
		return false, errors.New("invalid control modifier")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || actor.IsDefeated {
		return false, fmt.Errorf("controlMissing: %d", objectID)
	}
	immunity := actor.ControlImmunity(at)
	banishEnd := actor.status.banishExpiresAt
	for modifierID, contributionEnd := range actor.status.banishModifiers {
		if modifierID != instanceID && banishEnd.Before(contributionEnd) {
			banishEnd = contributionEnd
		}
	}
	if isStatusBlockedByShield(actor) || kind == "stun" && immunity.Stun > 0 ||
		kind == "silence" && immunity.Silence > 0 || kind == "sleep" && immunity.Sleep > 0 || kind == "banish" &&
		(immunity.Banish > 0 || actor.IsMutationBanishImmune() || at.Before(banishEnd) || at.Before(actor.status.intangibleExpiresAt)) {
		return false, nil
	}
	var contributions *map[uint32]time.Time
	switch kind {
	case "stun":
		contributions = &actor.status.stunModifiers
	case "silence":
		contributions = &actor.status.silenceModifiers
	case "banish":
		contributions = &actor.status.banishModifiers
	case "sleep":
		contributions = &actor.status.sleepModifiers
	case "root":
		contributions = &actor.status.rootModifiers
	default:
		return false, errors.New("unsupported control modifier")
	}
	// Earlier snapshots keep their own contribution maps.
	// Re-admission of the same handle refreshes only its own contribution.
	*contributions = maps.Clone(*contributions)
	if *contributions == nil {
		*contributions = make(map[uint32]time.Time)
	}
	(*contributions)[instanceID] = expiresAt
	if kind == "stun" || kind == "silence" || kind == "banish" {
		endDirectionalShield(&actor)
	}
	e.npcs[objectID] = actor
	return true, nil
}

func (e *Session) ClearStunModifier(objectID, instanceID uint32) {
	e.clearControlModifier(objectID, instanceID, "stun")
}

func (e *Session) ClearSilenceModifier(objectID, instanceID uint32) {
	e.clearControlModifier(objectID, instanceID, "silence")
}

func (e *Session) ClearBanishModifier(objectID, instanceID uint32) {
	e.clearControlModifier(objectID, instanceID, "banish")
}

func (e *Session) ClearSleepModifier(objectID, instanceID uint32) {
	e.clearControlModifier(objectID, instanceID, "sleep")
}

func (e *Session) ClearRootModifier(objectID, instanceID uint32) {
	e.clearControlModifier(objectID, instanceID, "root")
}

func (e *Session) clearControlModifier(objectID, instanceID uint32, kind string) {
	if e == nil || objectID == 0 || instanceID == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound {
		return
	}
	var contributions *map[uint32]time.Time
	switch kind {
	case "stun":
		contributions = &actor.status.stunModifiers
	case "silence":
		contributions = &actor.status.silenceModifiers
	case "banish":
		contributions = &actor.status.banishModifiers
	case "sleep":
		contributions = &actor.status.sleepModifiers
	case "root":
		contributions = &actor.status.rootModifiers
	default:
		return
	}
	if _, isTracked := (*contributions)[instanceID]; !isTracked {
		return
	}
	*contributions = maps.Clone(*contributions)
	delete(*contributions, instanceID)
	e.npcs[objectID] = actor
}

func controlEnd(expiresAt time.Time, contributions map[uint32]time.Time) time.Time {
	for _, contributionEnd := range contributions {
		if expiresAt.Before(contributionEnd) {
			expiresAt = contributionEnd
		}
	}
	return expiresAt
}

func (e status) stunEnd() time.Time {
	return controlEnd(e.stunExpiresAt, e.stunModifiers)
}

func (e status) banishEnd() time.Time {
	return controlEnd(e.banishExpiresAt, e.banishModifiers)
}

func (e status) rootEnd() time.Time {
	return controlEnd(e.rootExpiresAt, e.rootModifiers)
}

// AdmitTimeStopModifier implements build-103 chunk962: its single CanUse gate
// checks ImmuneToBanish only in chain mode, then adds Immobilized, Silence and
// Frozen together. StunRemaining represents the frozen actor's action block;
// native stun and silence immunity do not reject these authored attributes.
func (e *Session) AdmitTimeStopModifier(objectID, instanceID uint32, at, expiresAt time.Time, isChainGame bool) (bool, error) {
	if e == nil || objectID == 0 || instanceID == 0 || at.IsZero() || !expiresAt.After(at) {
		return false, errors.New("invalid time stop modifier")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || actor.IsDefeated {
		return false, fmt.Errorf("timeStopMissing: %d", objectID)
	}
	if isStatusBlockedByShield(actor) || isChainGame && actor.ControlImmunity(at).Banish > 0 {
		return false, nil
	}
	actor.status.stunModifiers = maps.Clone(actor.status.stunModifiers)
	if actor.status.stunModifiers == nil {
		actor.status.stunModifiers = make(map[uint32]time.Time)
	}
	actor.status.silenceModifiers = maps.Clone(actor.status.silenceModifiers)
	if actor.status.silenceModifiers == nil {
		actor.status.silenceModifiers = make(map[uint32]time.Time)
	}
	actor.status.stunModifiers[instanceID] = expiresAt
	actor.status.silenceModifiers[instanceID] = expiresAt
	actor.status.timeStopModifiers = maps.Clone(actor.status.timeStopModifiers)
	if actor.status.timeStopModifiers == nil {
		actor.status.timeStopModifiers = make(map[uint32]time.Time)
	}
	actor.status.timeStopModifiers[instanceID] = expiresAt
	endDirectionalShield(&actor)
	e.npcs[objectID] = actor
	return true, nil
}

func (e *Session) TimeStopEnd(objectID uint32) time.Time {
	if e == nil || objectID == 0 {
		return time.Time{}
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || actor.IsDefeated {
		return time.Time{}
	}
	return controlEnd(time.Time{}, actor.status.timeStopModifiers)
}

func (e *Session) ClearTimeStopModifier(objectID, instanceID uint32) {
	if e == nil || objectID == 0 || instanceID == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound {
		return
	}
	actor.status.timeStopModifiers = maps.Clone(actor.status.timeStopModifiers)
	actor.status.stunModifiers = maps.Clone(actor.status.stunModifiers)
	actor.status.silenceModifiers = maps.Clone(actor.status.silenceModifiers)
	delete(actor.status.timeStopModifiers, instanceID)
	delete(actor.status.stunModifiers, instanceID)
	delete(actor.status.silenceModifiers, instanceID)
	e.npcs[objectID] = actor
}
