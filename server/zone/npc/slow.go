package npc

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"time"
)

type slowModifier struct {
	expiresAt     time.Time
	movementScale float32
	attackScale   float32
	order         uint64
}

// ApplySlowModifier retains each exact modifier even while another slow owns
// the effective profile. Latest-expiry precedence remains unchanged; removing
// that owner exposes the next live contribution instead of clearing all slows.
func (e *Session) ApplySlowModifier(objectID, instanceID uint32, expiresAt time.Time, movementScale, attackScale float32) error {
	if e == nil || objectID == 0 || instanceID == 0 || expiresAt.IsZero() ||
		movementScale <= 0 || movementScale > 1 || attackScale <= 0 || attackScale > 1 ||
		math.IsNaN(float64(movementScale)) || math.IsInf(float64(movementScale), 0) ||
		math.IsNaN(float64(attackScale)) || math.IsInf(float64(attackScale), 0) {
		return errors.New("invalid npc slow modifier")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || actor.IsDefeated {
		return fmt.Errorf("slowModifierMissing: %d", objectID)
	}
	actor.status.slowModifiers = maps.Clone(actor.status.slowModifiers)
	if actor.status.slowModifiers == nil {
		actor.status.slowModifiers = make(map[uint32]slowModifier)
	}
	contribution, isTracked := actor.status.slowModifiers[instanceID]
	if !isTracked {
		// Native handles reuse slots with a new generation. A retired slot's
		// stale contribution cannot survive reuse or grow historical storage.
		for previousID := range actor.status.slowModifiers {
			if uint16(previousID) == uint16(instanceID) {
				delete(actor.status.slowModifiers, previousID)
			}
		}
		actor.status.slowNextOrder++
		contribution.order = actor.status.slowNextOrder
	}
	contribution.expiresAt = expiresAt
	contribution.movementScale = movementScale
	contribution.attackScale = attackScale
	actor.status.slowModifiers[instanceID] = contribution
	e.npcs[objectID] = actor
	return nil
}

func (e *Session) ClearSlowModifier(objectID, instanceID uint32) {
	if e == nil || objectID == 0 || instanceID == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound {
		return
	}
	if _, isTracked := actor.status.slowModifiers[instanceID]; !isTracked {
		return
	}
	actor.status.slowModifiers = maps.Clone(actor.status.slowModifiers)
	delete(actor.status.slowModifiers, instanceID)
	e.npcs[objectID] = actor
}

func (e status) slowEnd() time.Time {
	expiresAt := e.slowExpiresAt
	for _, contribution := range e.slowModifiers {
		if expiresAt.Before(contribution.expiresAt) {
			expiresAt = contribution.expiresAt
		}
	}
	return expiresAt
}

func (e status) slowProfile(at time.Time) SlowScales {
	selected := slowModifier{}
	if at.Before(e.slowExpiresAt) {
		selected = slowModifier{
			expiresAt: e.slowExpiresAt, movementScale: e.slowMovementScale,
			attackScale: e.slowAttackScale, order: e.slowOrder,
		}
	}
	for _, contribution := range e.slowModifiers {
		if !at.Before(contribution.expiresAt) {
			continue
		}
		if selected.expiresAt.Before(contribution.expiresAt) ||
			selected.expiresAt.Equal(contribution.expiresAt) && contribution.order < selected.order {
			selected = contribution
		}
	}
	if selected.expiresAt.IsZero() {
		return SlowScales{Movement: 1, Attack: 1}
	}
	return SlowScales{
		Movement: max(float32(.05), selected.movementScale),
		Attack:   max(float32(.05), selected.attackScale),
	}
}
