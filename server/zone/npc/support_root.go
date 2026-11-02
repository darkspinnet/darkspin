package npc

import (
	"errors"
	"time"
)

// SupportHealer_RootModifier is Unique independently of BioEntangle and legacy
// root deadlines. Its exact instance fences late expiry after replacement.
func (e *Session) ApplySupportHealerRoot(objectID, instanceID uint32, expiresAt time.Time) (bool, error) {
	if e == nil || objectID == 0 || instanceID == 0 || expiresAt.IsZero() {
		return false, errors.New("invalid support root")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	target, isFound := e.npcs[objectID]
	if !isFound || !target.IsPublished || target.IsDefeated || target.HitPoint <= 0 || isStatusBlockedByShield(target) {
		return false, nil
	}
	target.status.supportHealerRootInstanceID = instanceID
	target.status.supportHealerRootExpiresAt = expiresAt
	e.npcs[objectID] = target
	return true, nil
}

func (e *Session) ClearSupportHealerRoot(objectID, instanceID uint32) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	target, isFound := e.npcs[objectID]
	if !isFound || target.status.supportHealerRootInstanceID != instanceID {
		return
	}
	target.status.supportHealerRootInstanceID = 0
	target.status.supportHealerRootExpiresAt = time.Time{}
	e.npcs[objectID] = target
}

func (e *Session) IsSupportHealerRootCurrent(objectID, instanceID uint32, at time.Time) bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	target, isFound := e.npcs[objectID]
	return isFound && !target.IsDefeated && target.HitPoint > 0 && target.status.supportHealerRootInstanceID == instanceID && at.Before(target.status.supportHealerRootExpiresAt)
}
