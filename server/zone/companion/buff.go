package companion

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"time"
)

// ApplyBuffModifier replaces this exact instance's additive contribution.
func (e *Session) ApplyBuffModifier(objectID, instanceID uint32, buff Buff) error {
	err := e.ReplaceBuffModifier(objectID, instanceID, 0, buff)
	if err != nil {
		return fmt.Errorf("buffApply: %w", err)
	}
	return nil
}

// ReplaceBuffModifier transfers a refreshed modifier's contribution atomically.
// Full handles fence stale cleanup; reused native slots discard old generations.
func (e *Session) ReplaceBuffModifier(objectID, instanceID, previousID uint32, buff Buff) error {
	err := e.replaceBuffModifier(objectID, instanceID, previousID, buff, 0, false)
	if err != nil {
		return fmt.Errorf("buffReplace: %w", err)
	}
	return nil
}

// ReplaceBuffModifierWithHitPoint commits Enrage's buff and healing together.
func (e *Session) ReplaceBuffModifierWithHitPoint(objectID, instanceID, previousID uint32, buff Buff, hitPoint float32) error {
	if !isFinitePositive(hitPoint) {
		return errors.New("invalid companion buff hit point")
	}
	err := e.replaceBuffModifier(objectID, instanceID, previousID, buff, hitPoint, true)
	if err != nil {
		return fmt.Errorf("buffHeal: %w", err)
	}
	return nil
}

func (e *Session) replaceBuffModifier(objectID, instanceID, previousID uint32, buff Buff, hitPoint float32, isHealing bool) error {
	if e == nil || objectID == 0 || instanceID == 0 || !isFiniteBuff(buff) {
		return errors.New("invalid companion buff modifier")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || actor.HitPoint <= 0 {
		return errors.New("companion buff target unavailable")
	}
	if isHealing && hitPoint > actor.MaximumHitPoint {
		return errors.New("companion buff hit point exceeds capacity")
	}
	previousMovementBuff := actor.MovementSpeedBuff
	actor.buffModifiers = maps.Clone(actor.buffModifiers)
	if actor.buffModifiers == nil {
		actor.buffModifiers = make(map[uint32]Buff)
	}
	for currentID, current := range actor.buffModifiers {
		if currentID == previousID || uint16(currentID) == uint16(instanceID) {
			actor = addActorBuff(actor, current, -1)
			delete(actor.buffModifiers, currentID)
		}
	}
	actor = addActorBuff(actor, buff, 1)
	if !isFiniteBuff(Buff{DamageBuff: actor.DamageBuff, EnergyDamageBuff: actor.EnergyDamageBuff,
		AttackSpeed: actor.AttackSpeed, CooldownReduction: actor.CooldownReduction, MovementSpeedBuff: actor.MovementSpeedBuff}) {
		return errors.New("companion buff aggregate invalid")
	}
	actor.buffModifiers[instanceID] = buff
	actor = e.rebaseBuffMovementLocked(actor, previousMovementBuff)
	if isHealing {
		actor.HitPoint = min(actor.MaximumHitPoint, hitPoint)
	}
	e.storeCinematicActor(actor, true)
	return nil
}

func (e *Session) ClearBuffModifier(objectID, instanceID uint32) {
	if e == nil || objectID == 0 || instanceID == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound, isLive := e.cinematicActor(objectID)
	if !isFound {
		return
	}
	buff, isTracked := actor.buffModifiers[instanceID]
	if !isTracked {
		return
	}
	previousMovementBuff := actor.MovementSpeedBuff
	actor.buffModifiers = maps.Clone(actor.buffModifiers)
	delete(actor.buffModifiers, instanceID)
	actor = addActorBuff(actor, buff, -1)
	actor = e.rebaseBuffMovementLocked(actor, previousMovementBuff)
	e.storeCinematicActor(actor, isLive)
}

func (e *Session) rebaseBuffMovementLocked(actor Actor, previousBuff float32) Actor {
	if actor.MovementSpeedBuff == previousBuff {
		return actor
	}
	previousScale := max(float32(.1), 1+previousBuff+actor.movementModifierBuff)
	scale := movementScale(actor)
	return e.rebaseMovementScaleLocked(actor, time.Now(), previousScale, scale)
}

func addActorBuff(actor Actor, buff Buff, scale float32) Actor {
	actor.DamageBuff += scale * buff.DamageBuff
	actor.EnergyDamageBuff += scale * buff.EnergyDamageBuff
	actor.AttackSpeed += scale * buff.AttackSpeed
	actor.CooldownReduction += scale * buff.CooldownReduction
	actor.MovementSpeedBuff += scale * buff.MovementSpeedBuff
	return actor
}

func clearActorBuffModifiers(actor Actor) Actor {
	for _, buff := range actor.buffModifiers {
		actor = addActorBuff(actor, buff, -1)
	}
	actor.buffModifiers = nil
	if actor.maximumHitPointModifierID != 0 {
		actor.MaximumHitPoint = max(float32(1), actor.maximumHitPointBase)
		actor.HitPoint = min(actor.HitPoint, actor.MaximumHitPoint)
		actor.maximumHitPointModifierID = 0
		actor.maximumHitPointBase = 0
	}
	return actor
}

func isFiniteBuff(buff Buff) bool {
	for _, amount := range [...]float32{buff.DamageBuff, buff.EnergyDamageBuff, buff.AttackSpeed, buff.CooldownReduction, buff.MovementSpeedBuff} {
		if math.IsNaN(float64(amount)) || math.IsInf(float64(amount), 0) {
			return false
		}
	}
	return true
}
