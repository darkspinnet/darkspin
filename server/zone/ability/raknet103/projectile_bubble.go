package raknet103

import "time"

// Each bubble retains an exact lease. The underlying scale remains owned by
// SetSpeedScale, including updates made while this override is active.
type ProjectileTimeBubbleLease struct {
	run *ProjectileRun
}

func (e *ProjectileRun) effectiveSpeedScaleLocked() float32 {
	if len(e.timeBubbleLeases) != 0 {
		return 0.40
	}
	return e.motionSpeedScale
}

func (e *ProjectileRun) AcquireTimeBubble(now time.Time) *ProjectileTimeBubbleLease {
	if e == nil || now.IsZero() {
		return nil
	}
	e.motionMutex.Lock()
	defer e.motionMutex.Unlock()
	e.advanceMotionLocked(now)
	if e.isFinished || e.motionRemaining <= 0 || e.behavior == nil || !e.behavior.IsProjectileActive() {
		return nil
	}
	lease := &ProjectileTimeBubbleLease{run: e}
	if e.timeBubbleLeases == nil {
		e.timeBubbleLeases = make(map[*ProjectileTimeBubbleLease]struct{})
	}
	e.timeBubbleLeases[lease] = struct{}{}
	return lease
}

// PreviewTimeBubble prepares trajectory presentation before committing a
// membership change. It samples motion without changing any speed lease.
func (e *ProjectileRun) PreviewTimeBubble(now time.Time, removed *ProjectileTimeBubbleLease, isAdding bool) ProjectileSnapshot {
	if e == nil || now.IsZero() || e.behavior == nil || !e.behavior.IsProjectileActive() {
		return ProjectileSnapshot{}
	}
	e.motionMutex.Lock()
	defer e.motionMutex.Unlock()
	snapshot := e.snapshotLocked(now)
	if e.isFinished {
		return ProjectileSnapshot{}
	}
	if !snapshot.IsActive {
		return snapshot
	}
	leaseCount := len(e.timeBubbleLeases)
	if _, isFound := e.timeBubbleLeases[removed]; isFound {
		leaseCount--
	}
	scale := e.motionSpeedScale
	if isAdding || leaseCount > 0 {
		scale = 0.40
	}
	snapshot.Speed = e.speed * scale
	return snapshot
}

func (e *ProjectileTimeBubbleLease) Release(now time.Time) {
	if e == nil || e.run == nil || now.IsZero() {
		return
	}
	e.run.motionMutex.Lock()
	defer e.run.motionMutex.Unlock()
	if _, isFound := e.run.timeBubbleLeases[e]; !isFound {
		return
	}
	e.run.advanceMotionLocked(now)
	delete(e.run.timeBubbleLeases, e)
}
