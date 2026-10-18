package sim

import "time"

func (e *ProjectileBurstBehavior) shotFlightDuration(index int, elapsed time.Duration) time.Duration {
	shot := e.shots[index]
	return shot.motionElapsed + time.Duration(float64(max(time.Duration(0), elapsed-shot.motionUpdated))*float64(shot.speedScale))
}

// SetShotSpeed retains distance already traveled before applying a field slow.
func (e *ProjectileBurstBehavior) SetShotSpeed(index int, elapsed time.Duration, scale float32) bool {
	if e == nil || e.isCanceled || index < 0 || index >= len(e.shots) || scale <= 0 {
		return false
	}
	shot := &e.shots[index]
	if !shot.isLaunched || shot.isResolved || elapsed < shot.motionUpdated {
		return false
	}
	shot.motionElapsed = e.shotFlightDuration(index, elapsed)
	shot.motionUpdated = elapsed
	shot.speedScale = scale
	return true
}

func (e *ProjectileBurstBehavior) RemainingShotDelay(index int, elapsed, deadline time.Duration) time.Duration {
	if e == nil || e.isCanceled || index < 0 || index >= len(e.shots) {
		return 0
	}
	shot := e.shots[index]
	if shot.isResolved || shot.speedScale <= 0 {
		return 0
	}
	remaining := deadline - e.input.ShotDelays[index] - e.shotFlightDuration(index, elapsed)
	return time.Duration(float64(max(time.Duration(0), remaining)) / float64(shot.speedScale))
}
