package gameplay

import (
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

// Lob time runs slowly in a shield, then resumes normal time for ground fuses.
type graviticLob struct {
	mutex     sync.Mutex
	updatedAt time.Time
	elapsed   time.Duration
	scale     float32
	origin    sim.Position
	lob       sim.CrystalLob
}

func newGraviticLob(now time.Time, origin sim.Position, lob sim.CrystalLob) *graviticLob {
	return &graviticLob{updatedAt: now, scale: 1, origin: origin, lob: lob}
}

func (e *graviticLob) advance(now time.Time) {
	if !now.After(e.updatedAt) {
		return
	}
	delta := now.Sub(e.updatedAt)
	if e.elapsed < e.lob.Duration {
		flight := min(delta, time.Duration(float64(e.lob.Duration-e.elapsed)/float64(e.scale)))
		e.elapsed += time.Duration(float64(flight) * float64(e.scale))
		delta -= flight
	}
	e.elapsed += delta
	e.updatedAt = now
}

func (e *graviticLob) setSpeed(now time.Time, scale float32) bool {
	if e == nil || scale <= 0 {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.advance(now)
	if e.elapsed >= e.lob.Duration {
		return false
	}
	e.scale = scale
	return true
}

func (e *graviticLob) position(now time.Time) (game.Vec3, bool) {
	if e == nil {
		return game.Vec3{}, false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.advance(now)
	seconds := float32(e.elapsed.Seconds())
	distance := e.lob.PlaneDirectionVelocity * seconds
	return game.Vec3{
		X: e.origin.X + e.lob.PlaneDirection.X*distance,
		Y: e.origin.Y + e.lob.PlaneDirection.Y*distance,
		Z: e.origin.Z + e.lob.UpLinearParameter*distance + e.lob.UpQuadraticParameter*distance*distance,
	}, e.elapsed < e.lob.Duration
}

func (e *graviticLob) remaining(now time.Time, groundDelay time.Duration) time.Duration {
	if e == nil {
		return 0
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.advance(now)
	if e.elapsed >= e.lob.Duration {
		return max(time.Duration(0), e.lob.Duration+groundDelay-e.elapsed)
	}
	return time.Duration(float64(e.lob.Duration-e.elapsed)/float64(e.scale)) + groundDelay
}
