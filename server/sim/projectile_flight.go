package sim

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// ProjectileFlight owns a straight shot's travel between collision samples.
// Targets are sampled at each update, as in the native dynamic-object query.
type ProjectileFlight struct {
	position        Position
	direction       Position
	halfExtent      Position
	speed           float32
	acceleration    float32
	maximumDistance float32
	distance        float32
	at              time.Duration
	duration        time.Duration
	isResolved      bool
}

type ProjectileFlightInput struct {
	Position        Position
	Direction       Position
	HalfExtent      Position
	Speed           float32
	Acceleration    float32
	MaximumDistance float32
}

type ProjectileCollisionTarget struct {
	ObjectID uint32
	Position Position
	Minimum  Position
	Maximum  Position
}

type ProjectileFlightResult struct {
	Position       Position
	TargetObjectID uint32
	Distance       float32
	Speed          float32
	IsResolved     bool
}

// ProjectileMotionSegment records uninterrupted travel between changes to
// direction or speed. Collision must sweep each segment, not the chord between
// the first and last positions of a deflected projectile.
type ProjectileMotionSegment struct {
	Position     Position
	Direction    Position
	Distance     float32
	Speed        float32
	Acceleration float32
}

// AdvanceMotion consumes the same travel that drives projectile presentation.
// Frozen intervals have no segments; slow and deflection are already reflected
// in each segment. Range expiry still precedes collision in the current sample.
func (e *ProjectileFlight) AdvanceMotion(
	at time.Duration, segments []ProjectileMotionSegment, isExpired bool, targets []ProjectileCollisionTarget,
) (ProjectileFlightResult, error) {
	if e == nil || at < e.at {
		return ProjectileFlightResult{}, errors.New("invalid projectile clock")
	}
	result := ProjectileFlightResult{Position: e.position, Distance: e.distance, IsResolved: e.isResolved}
	if e.isResolved {
		return result, nil
	}
	for _, segment := range segments {
		if !isFinitePosition(segment.Position) || !isFinitePosition(segment.Direction) ||
			segment.Distance <= 0 || math.IsNaN(float64(segment.Distance)) || math.IsInf(float64(segment.Distance), 0) ||
			segment.Speed < 0 || math.IsNaN(float64(segment.Speed)) || math.IsInf(float64(segment.Speed), 0) ||
			segment.Acceleration < 0 || math.IsNaN(float64(segment.Acceleration)) || math.IsInf(float64(segment.Acceleration), 0) {
			return ProjectileFlightResult{}, errors.New("invalid projectile motion")
		}
		result.Distance += segment.Distance
		result.Position = addPosition(segment.Position, scalePosition(segment.Direction, segment.Distance))
		result.Speed = float32(math.Sqrt(float64(segment.Speed*segment.Speed + 2*segment.Acceleration*segment.Distance)))
	}
	result.IsResolved = isExpired || result.Distance >= e.maximumDistance
	if !result.IsResolved {
		travelled := e.distance
		for _, segment := range segments {
			contactDistance := segment.Distance
			for _, target := range targets {
				if target.ObjectID == 0 {
					return ProjectileFlightResult{}, errors.New("invalid collision target")
				}
				collision, err := ResolveProjectileBoxCollision(ProjectileBoxCollisionInput{
					ProjectilePosition: segment.Position, ProjectileDirection: segment.Direction,
					ProjectileHalfExtent: e.halfExtent, TargetPosition: target.Position,
					TargetMinimum: target.Minimum, TargetMaximum: target.Maximum,
					MaximumDistance: e.maximumDistance - travelled,
				})
				if err != nil {
					return ProjectileFlightResult{}, fmt.Errorf("motionTarget[%d]: %w", target.ObjectID, err)
				}
				if !collision.IsDirectHit || collision.TravelDistance > contactDistance ||
					(collision.TravelDistance == contactDistance && result.TargetObjectID != 0 && target.ObjectID > result.TargetObjectID) {
					continue
				}
				contactDistance = collision.TravelDistance
				result.Position = collision.Position
				result.TargetObjectID = target.ObjectID
				result.Distance = travelled + contactDistance
				result.Speed = float32(math.Sqrt(float64(segment.Speed*segment.Speed + 2*segment.Acceleration*contactDistance)))
				result.IsResolved = true
			}
			if result.IsResolved {
				break
			}
			travelled += segment.Distance
		}
	}
	e.position = result.Position
	e.distance = result.Distance
	e.at = at
	e.isResolved = result.IsResolved
	return result, nil
}

func (e *ProjectileFlight) IsResolved() bool {
	return e != nil && e.isResolved
}

func (e *ProjectileFlight) Duration() time.Duration {
	return e.duration
}

func NewProjectileFlight(req ProjectileFlightInput) (*ProjectileFlight, error) {
	if !isFinitePosition(req.Position) || !isFinitePosition(req.HalfExtent) ||
		req.HalfExtent.X <= 0 || req.HalfExtent.Y <= 0 || req.HalfExtent.Z <= 0 {
		return nil, errors.New("invalid projectile shape")
	}
	if req.Speed <= 0 || req.Acceleration < 0 || req.MaximumDistance <= 0 ||
		math.IsNaN(float64(req.Speed)) || math.IsInf(float64(req.Speed), 0) ||
		math.IsNaN(float64(req.Acceleration)) || math.IsInf(float64(req.Acceleration), 0) ||
		math.IsNaN(float64(req.MaximumDistance)) || math.IsInf(float64(req.MaximumDistance), 0) {
		return nil, errors.New("invalid projectile travel")
	}
	direction, err := normalizePosition(req.Direction)
	if err != nil {
		return nil, fmt.Errorf("flightDirection: %w", err)
	}
	finalSpeed := math.Sqrt(float64(req.Speed)*float64(req.Speed) +
		2*float64(req.Acceleration)*float64(req.MaximumDistance))
	nanoseconds := math.Ceil(2 * float64(req.MaximumDistance) /
		(float64(req.Speed) + finalSpeed) * float64(time.Second))
	if nanoseconds >= float64(math.MaxInt64) {
		return nil, errors.New("projectile lifetime overflow")
	}
	return &ProjectileFlight{
		position: req.Position, direction: direction, halfExtent: req.HalfExtent,
		speed: req.Speed, acceleration: req.Acceleration, maximumDistance: req.MaximumDistance,
		duration: time.Duration(nanoseconds),
	}, nil
}

// Advance sweeps only travel since the preceding sample and retains the first
// contact. Expiry wins when this physics update exhausts the range, matching
// WaitForProjectile's range-before-collision continuation.
func (e *ProjectileFlight) Advance(
	at time.Duration, targets []ProjectileCollisionTarget,
) (ProjectileFlightResult, error) {
	if e == nil || at < e.at {
		return ProjectileFlightResult{}, errors.New("invalid projectile clock")
	}
	result := ProjectileFlightResult{
		Position: e.position, Distance: e.distance, IsResolved: e.isResolved,
		Speed: e.speed + e.acceleration*float32(at.Seconds()),
	}
	if e.isResolved || at == e.at {
		return result, nil
	}
	seconds := float32(at.Seconds())
	distance := min(e.maximumDistance, e.speed*seconds+0.5*e.acceleration*seconds*seconds)
	if at >= e.duration {
		distance = e.maximumDistance
	}
	stepDistance := distance - e.distance
	if stepDistance <= 0 {
		e.at = at
		return result, nil
	}
	result.Position = addPosition(e.position, scalePosition(e.direction, stepDistance))
	result.Distance = distance
	result.IsResolved = distance >= e.maximumDistance
	if !result.IsResolved {
		contactDistance := stepDistance
		for _, target := range targets {
			if target.ObjectID == 0 {
				return ProjectileFlightResult{}, errors.New("invalid collision target")
			}
			collision, err := ResolveProjectileBoxCollision(ProjectileBoxCollisionInput{
				ProjectilePosition: e.position, ProjectileDirection: e.direction,
				ProjectileHalfExtent: e.halfExtent, TargetPosition: target.Position,
				TargetMinimum: target.Minimum, TargetMaximum: target.Maximum,
				MaximumDistance: e.maximumDistance - e.distance,
			})
			if err != nil {
				return ProjectileFlightResult{}, fmt.Errorf("flightTarget[%d]: %w", target.ObjectID, err)
			}
			if !collision.IsDirectHit || collision.TravelDistance > contactDistance {
				continue
			}
			if collision.TravelDistance == contactDistance && result.TargetObjectID != 0 &&
				target.ObjectID > result.TargetObjectID {
				continue
			}
			contactDistance = collision.TravelDistance
			result.Position = collision.Position
			result.TargetObjectID = target.ObjectID
			result.Distance = e.distance + contactDistance
			result.IsResolved = true
		}
	}
	if result.IsResolved {
		// v^2 = u^2 + 2as gives damage-at-speed the actual contact speed.
		result.Speed = float32(math.Sqrt(float64(
			e.speed*e.speed + 2*e.acceleration*result.Distance,
		)))
	}
	e.position = result.Position
	e.distance = result.Distance
	e.at = at
	e.isResolved = result.IsResolved
	return result, nil
}
