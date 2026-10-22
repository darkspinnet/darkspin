package gameplay

import (
	"fmt"
	"time"
)

// LifePlague and LifePlagueSpread inherit DOT149.GetRemainingDamage. Their
// Deactivate callbacks apply disease immunity; BioEntangle is a separate
// modifier and must retain its own expiry after Expunge removes the disease.
func (e *heroInfectionRun) consumeExpunge(targetObjectID uint32, at time.Time) (*expungeRemainingDamage, *expungeModifierDelete, error) {
	if e == nil {
		return nil, nil, nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	entry, isFound := e.infections[targetObjectID]
	if !isFound || entry.isConsumed || e.isCleaned || !e.isAccepted || e.definition.TickDuration <= 0 {
		return nil, nil, nil
	}
	remaining := max(time.Duration(0), entry.expiresAt.Sub(at))
	tickCount := uint32(remaining / e.definition.TickDuration)
	if remaining%e.definition.TickDuration != 0 {
		tickCount++
	}
	minimum := e.definition.MinimumDamagePerTick
	maximum := e.definition.MaximumDamagePerTick
	if !entry.isPrimary {
		minimum = e.definition.SecondaryMinimumDamage
		maximum = e.definition.SecondaryMaximumDamage
	}
	deleted := &expungeModifierDelete{targetObjectID: targetObjectID, instanceID: entry.modifierInstanceID}
	e.applyImmunity(entry, at)
	if entry.rootInstanceID == 0 {
		delete(e.infections, targetObjectID)
	} else {
		entry.isConsumed = true
		entry.modifierInstanceID = 0
		e.infections[targetObjectID] = entry
	}
	err := e.modifierPool.Release(deleted.instanceID)
	if err != nil {
		return nil, nil, fmt.Errorf("infectionRelease: %w", err)
	}
	if tickCount == 0 {
		return nil, deleted, nil
	}
	damage := &expungeRemainingDamage{
		creature: e.creature, sourceObjectID: e.sourceObjectID,
		definition: expungeDamageDefinition(e.definition, tickCount, minimum, maximum),
	}
	return damage, deleted, nil
}
