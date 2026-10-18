package sim

import "time"

// DropFlight retains the original wire clock and lob parameters. Projectile
// definitions use their separate collision locomotion; this updater never
// probes terrain or creatures and never consumes bounce randomness.
type DropFlight struct {
	Source              Position
	Destination         Position
	Lob                 CrystalLob
	StartedAt           time.Time
	Position            Position
	Movement            Position
	MovementType        uint8
	IsProjectilePresent bool
}

func (e DropFlight) At(now time.Time) DropFlight {
	if e.StartedAt.IsZero() || e.IsProjectilePresent || e.MovementType == 6 {
		return e
	}
	elapsed := now.Sub(e.StartedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	// The native elapsed clock is millisecond precision.
	elapsedMilliseconds := int32(uint32(elapsed / time.Millisecond))
	if elapsedMilliseconds < 0 {
		elapsedMilliseconds = 0
	}
	elapsed = time.Duration(elapsedMilliseconds) * time.Millisecond
	if elapsed >= e.Lob.Duration {
		e.Position = e.Destination
		e.Movement = Position{}
		e.MovementType = 6
		return e
	}
	u := e.Lob.PlaneDirectionVelocity * (float32(elapsedMilliseconds) * float32(0.001))
	height := (e.Lob.UpQuadraticParameter*u + e.Lob.UpLinearParameter) * u
	position := Position{
		X: e.Source.X + e.Lob.PlaneDirection.X*u,
		Y: e.Source.Y + e.Lob.PlaneDirection.Y*u,
		Z: e.Source.Z + e.Lob.PlaneDirection.Z*u + height,
	}
	e.Movement = Position{X: position.X - e.Position.X, Y: position.Y - e.Position.Y, Z: position.Z - e.Position.Z}
	e.Position = position
	e.MovementType = 4
	return e
}
