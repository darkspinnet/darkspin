package gameplay

import "time"

// Sleeping Cloud is a fixed-duration cast; releasing its key does not retire
// the field. Keep predicted movement from moving its stationary caster.
func (e gameplayPeerSession) isSleepingCloudActive(now time.Time) bool {
	for _, run := range e.heroAuraAreas {
		if run == nil || !run.isSleepingCloud ||
			run.cast.sourceObjectID != e.deployedObjectID || !now.Before(run.expiresAt) {
			continue
		}
		run.mutex.Lock()
		isActive := !run.isCleaned && !run.isStopping
		run.mutex.Unlock()
		if isActive {
			return true
		}
	}
	return false
}

// The registry lock is held and the retiring run's mutex has been released.
// Share native Immobilized ownership with roots/stuns so one ending effect
// cannot release a hero still held by another effect.
func (e *heroAuraAreaRun) refreshSleepingCloudControlLocked() {
	if !e.isSleepingCloud {
		return
	}
	for sessionKey, peerSession := range e.runtime.registry.sessions {
		if peerSession.zone != e.originalZone || peerSession.generation != e.cast.generation ||
			peerSession.binding.UserID != e.cast.userID {
			continue
		}
		packets, err := peerSession.refreshEnemyImmobilizedAttributes(e.runtime.now())
		if err != nil {
			e.runtime.logger.Printf("RakNet Sleeping Cloud movement release failed: %v", err)
			return
		}
		e.runtime.registry.sessions[sessionKey] = peerSession
		e.runtime.registry.queueProjectileRetirementLocked(e.originalZone, packets)
		return
	}
}
