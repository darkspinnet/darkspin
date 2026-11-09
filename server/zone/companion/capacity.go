package companion

import "errors"

// ApplyMaximumHitPointPercent owns FieldMedicHealthBuff829's Unique
// kPercentChange contribution. Only one such modifier is admitted at a time;
// mixed attribute-operation ordering is deliberately not modeled here.
func (e *Session) ApplyMaximumHitPointPercent(objectID, instanceID uint32, percent float32) error {
	if e == nil || objectID == 0 || instanceID == 0 || !isFiniteNonNegative(percent) {
		return errors.New("invalid companion capacity modifier")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || actor.HitPoint <= 0 {
		return errors.New("companion capacity target unavailable")
	}
	if actor.maximumHitPointModifierID == 0 {
		actor.maximumHitPointBase = actor.MaximumHitPoint
	}
	capacity := actor.maximumHitPointBase * (1 + percent)
	if !isFinitePositive(capacity) {
		return errors.New("companion capacity aggregate invalid")
	}
	actor.maximumHitPointModifierID = instanceID
	actor.MaximumHitPoint = capacity
	// Increasing capacity preserves current health, as the existing owner did.
	actor.HitPoint = min(actor.HitPoint, capacity)
	e.storeCinematicActor(actor, true)
	return nil
}

func (e *Session) ClearMaximumHitPointPercent(objectID, instanceID uint32) {
	if e == nil || objectID == 0 || instanceID == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound, isLive := e.cinematicActor(objectID)
	if !isFound || actor.maximumHitPointModifierID != instanceID {
		return
	}
	actor.MaximumHitPoint = max(float32(1), actor.maximumHitPointBase)
	actor.maximumHitPointModifierID = 0
	actor.maximumHitPointBase = 0
	actor.HitPoint = min(actor.HitPoint, actor.MaximumHitPoint)
	e.storeCinematicActor(actor, isLive)
}
