package raknet103

import (
	"errors"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
)

// SteerHomingTarget preserves travelled segments before changing direction.
// The native projectile already tracks this target; collision must follow the
// same moving target rather than resolve its entire launch ray on a fixed timer.
func (e *ProjectileRun) SteerHomingTarget(now time.Time, target sim.Position) error {
	if e == nil || now.IsZero() {
		return errors.New("homing clock unavailable")
	}
	for _, coordinate := range []float32{target.X, target.Y, target.Z} {
		if math.IsNaN(float64(coordinate)) || math.IsInf(float64(coordinate), 0) {
			return errors.New("homing target is nonfinite")
		}
	}
	e.motionMutex.Lock()
	defer e.motionMutex.Unlock()
	if !e.isCollisionSampled || e.homingDelay <= 0 || e.isFinished || e.isGravityDeflected ||
		now.Before(e.launchAt.Add(e.homingDelay)) || now.Before(e.frozenUntil) {
		return nil
	}
	e.advanceMotionLocked(now)
	delta := sim.Position{X: target.X - e.motionPosition.X, Y: target.Y - e.motionPosition.Y, Z: target.Z - e.motionPosition.Z}
	length := float32(math.Sqrt(float64(delta.X*delta.X + delta.Y*delta.Y + delta.Z*delta.Z)))
	if length > 0 {
		e.direction = sim.Position{X: delta.X / length, Y: delta.Y / length, Z: delta.Z / length}
	}
	e.targetPosition = target
	return nil
}
