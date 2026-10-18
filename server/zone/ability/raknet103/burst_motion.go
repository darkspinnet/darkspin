package raknet103

import "time"

func (e *BurstRun) SetSpeedScale(objectID uint32, now time.Time, scale float32) bool {
	if e == nil || e.behavior == nil {
		return false
	}
	for index, projectileID := range e.projectileObjectIDs {
		if projectileID == objectID {
			return e.behavior.SetShotSpeed(index, now.Sub(e.startedAt), scale)
		}
	}
	return false
}

func (e *BurstRun) RemainingFlightDelay(index int, now time.Time, deadline time.Duration) time.Duration {
	if e == nil || e.behavior == nil {
		return 0
	}
	return e.behavior.RemainingShotDelay(index, now.Sub(e.startedAt), deadline)
}
