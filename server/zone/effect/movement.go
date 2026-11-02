package effect

import "time"

// MovementDebuff exposes only active negative movement contributions, leaving
// separately owned companion haste buffs out of this read projection.
func (e *Inventory) MovementDebuff(objectID uint32, at time.Time) (float32, time.Time) {
	if e == nil {
		return 0, time.Time{}
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	buff := float32(0)
	nextExpiry := time.Time{}
	for _, modifier := range e.modifiers {
		if modifier.TargetObjectID != objectID || modifier.Kind != ModifierKindDebuff || modifier.MovementSpeedBuff >= 0 {
			continue
		}
		if !modifier.ExpiresAt.IsZero() {
			if !at.Before(modifier.ExpiresAt) || at.Before(modifier.ExpiresAt.Add(-modifier.Duration)) {
				continue
			}
			if nextExpiry.IsZero() || modifier.ExpiresAt.Before(nextExpiry) {
				nextExpiry = modifier.ExpiresAt
			}
		}
		buff += modifier.MovementSpeedBuff
	}
	return buff, nextExpiry
}
