package npc

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// ControlImmunity contains supported own-attribute contributions, independent
// of the generic modifier-rejection gate used by defensive stances.
type ControlImmunity struct {
	Sleep   float32
	Silence float32
	Stun    float32
	Taunt   float32
	Fear    float32
	Banish  float32
}

func (e Snapshot) ControlImmunity(at time.Time) ControlImmunity {
	immunity := ControlImmunity{}
	// UniqueIrreplaceable Unstoppable is one recipient modifier, including
	// when several aura sources supply it; do not count supplying sources.
	if e.HasAffixModifier(unstoppableModifierName) {
		immunity.Sleep = 1
		immunity.Silence = 1
		immunity.Stun = 1
		immunity.Taunt = 1
		immunity.Fear = 1
	}
	// Build-103 modifier159 contributes these exact own attributes while
	// active. Its knockback, random-teleport and pull attributes have no
	// consumer here; it does not grant root or slow immunity.
	if !at.IsZero() && at.Before(e.status.verdanthShieldExpiresAt) {
		immunity.Sleep++
		immunity.Silence++
		immunity.Stun++
		immunity.Taunt++
		immunity.Fear++
		immunity.Banish++
	}
	return immunity
}

// ApplyVerdanthShield owns modifier159's immunity lifetime separately from
// generic damage reduction. Only callers that apply this modifier use it.
func (e *Session) ApplyVerdanthShield(objectID uint32, reduction float32, expiresAt time.Time) error {
	if e == nil || objectID == 0 || reduction <= 0 || reduction >= 1 ||
		math.IsNaN(float64(reduction)) || expiresAt.IsZero() {
		return errors.New("invalid verdanth shield")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || actor.IsDefeated {
		return fmt.Errorf("verdanthShieldMissing: %d", objectID)
	}
	if !actor.status.damageReductionEnd.After(expiresAt) {
		actor.status.damageReduction = reduction
		actor.status.damageReductionEnd = expiresAt
	}
	if actor.status.verdanthShieldExpiresAt.Before(expiresAt) {
		actor.status.verdanthShieldExpiresAt = expiresAt
	}
	e.npcs[objectID] = actor
	return nil
}

// ClearVerdanthShield uses the accepted expiry token so an old completion or
// rollback cannot remove a later renewal. Damage reduction retains its existing
// expiry-token ownership contract; immunity has its own distinct token.
func (e *Session) ClearVerdanthShield(objectID uint32, expiresAt time.Time) {
	if e == nil || objectID == 0 || expiresAt.IsZero() {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound {
		return
	}
	if actor.status.verdanthShieldExpiresAt == expiresAt {
		actor.status.verdanthShieldExpiresAt = time.Time{}
	}
	if actor.status.damageReductionEnd == expiresAt {
		actor.status.damageReductionEnd = time.Time{}
		actor.status.damageReduction = 0
	}
	e.npcs[objectID] = actor
}
