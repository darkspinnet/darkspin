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
