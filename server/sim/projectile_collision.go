package sim

import (
	"errors"
	"fmt"
	"math"
)

// ProjectileBoxCollisionInput describes one authoritative straight projectile
// query against an axis-aligned target. ProjectileHalfExtent is the native
// query box half-extent; TargetMinimum and TargetMaximum are relative to the
// target object's position.
type ProjectileBoxCollisionInput struct {
	ProjectilePosition   Position
	ProjectileDirection  Position
	ProjectileHalfExtent Position
	TargetPosition       Position
	TargetMinimum        Position
	TargetMaximum        Position
	MaximumDistance      float32
}

// ProjectileBoxCollision is the first direct contact, or the exact range
// expiry fallback when no contact occurs strictly before MaximumDistance.
type ProjectileBoxCollision struct {
	Position           Position
	ExitPosition       Position
	TravelDistance     float32
	ExitTravelDistance float32
	IsDirectHit        bool
}

// ProjectileBoxTracker retains WaitForProjectile-style state across
// authoritative simulation updates. Each step samples the target's latest
// position and sweeps only that update's projectile displacement.
type ProjectileBoxTracker struct {
	position             Position
	direction            Position
	projectileHalfExtent Position
	targetMinimum        Position
	targetMaximum        Position
	remainingDistance    float32
	isResolved           bool
}

func NewProjectileBoxTracker(input ProjectileBoxCollisionInput) (*ProjectileBoxTracker, error) {
	_, err := ResolveProjectileBoxCollision(input)
	if err != nil {
		return nil, fmt.Errorf("inputValidate: %w", err)
	}
	direction, err := normalizePosition(input.ProjectileDirection)
	if err != nil {
		return nil, fmt.Errorf("directionNormalize: %w", err)
	}
	return &ProjectileBoxTracker{
		position: input.ProjectilePosition, direction: direction,
		projectileHalfExtent: input.ProjectileHalfExtent,
		targetMinimum:        input.TargetMinimum, targetMaximum: input.TargetMaximum,
		remainingDistance: input.MaximumDistance,
	}, nil
}

// Advance applies one physics step. Range exhaustion is evaluated before the
// collision flags, matching WaitForProjectile: a step that consumes all
// remaining travel returns fallback even if its sweep also found a contact.
func (t *ProjectileBoxTracker) Advance(
	travelDistance float32, targetPosition Position, isTargetEligible bool,
) (ProjectileBoxCollision, bool, error) {
	if t == nil {
		return ProjectileBoxCollision{}, false, errors.New("nil projectile tracker")
	}
	if t.isResolved {
		return ProjectileBoxCollision{}, false, errors.New("projectile tracker resolved")
	}
	if travelDistance <= 0 || math.IsNaN(float64(travelDistance)) || math.IsInf(float64(travelDistance), 0) {
		return ProjectileBoxCollision{}, false, fmt.Errorf("travel distance: %g", travelDistance)
	}
	if !isFinitePosition(targetPosition) {
		return ProjectileBoxCollision{}, false, errors.New("non-finite target position")
	}
	if travelDistance >= t.remainingDistance {
		t.position = addPosition(t.position, scalePosition(t.direction, t.remainingDistance))
		t.remainingDistance = 0
		t.isResolved = true
		return ProjectileBoxCollision{Position: t.position}, true, nil
	}
	if isTargetEligible {
		expandedMinimum := Position{
			X: targetPosition.X + t.targetMinimum.X - t.projectileHalfExtent.X,
			Y: targetPosition.Y + t.targetMinimum.Y - t.projectileHalfExtent.Y,
			Z: targetPosition.Z + t.targetMinimum.Z - t.projectileHalfExtent.Z,
		}
		expandedMaximum := Position{
			X: targetPosition.X + t.targetMaximum.X + t.projectileHalfExtent.X,
			Y: targetPosition.Y + t.targetMaximum.Y + t.projectileHalfExtent.Y,
			Z: targetPosition.Z + t.targetMaximum.Z + t.projectileHalfExtent.Z,
		}
		contactDistance, exitDistance, isContact := rayBoxContactDistance(
			t.position, t.direction, expandedMinimum, expandedMaximum,
		)
		if isContact && contactDistance <= travelDistance {
			t.position = addPosition(t.position, scalePosition(t.direction, contactDistance))
			t.remainingDistance -= contactDistance
			t.isResolved = true
			return ProjectileBoxCollision{
				Position:       t.position,
				ExitPosition:   addPosition(t.position, scalePosition(t.direction, exitDistance-contactDistance)),
				TravelDistance: contactDistance, ExitTravelDistance: exitDistance,
				IsDirectHit: true,
			}, true, nil
		}
	}
	t.position = addPosition(t.position, scalePosition(t.direction, travelDistance))
	t.remainingDistance -= travelDistance
	return ProjectileBoxCollision{Position: t.position, TravelDistance: travelDistance}, false, nil
}

// ResolveProjectileBoxCollision performs the build-103 dynamic-object query
// ordering for a stationary target: overlap at the current projectile position
// first, then a continuous box sweep. A contact exactly at the range boundary
// loses to range expiry.
func ResolveProjectileBoxCollision(input ProjectileBoxCollisionInput) (ProjectileBoxCollision, error) {
	if !isFinitePosition(input.ProjectilePosition) ||
		!isFinitePosition(input.ProjectileDirection) ||
		!isFinitePosition(input.ProjectileHalfExtent) ||
		!isFinitePosition(input.TargetPosition) ||
		!isFinitePosition(input.TargetMinimum) ||
		!isFinitePosition(input.TargetMaximum) {
		return ProjectileBoxCollision{}, errors.New("non-finite projectile collision input")
	}
	if input.MaximumDistance <= 0 || math.IsNaN(float64(input.MaximumDistance)) ||
		math.IsInf(float64(input.MaximumDistance), 0) ||
		input.ProjectileHalfExtent.X <= 0 ||
		input.ProjectileHalfExtent.Y <= 0 ||
		input.ProjectileHalfExtent.Z <= 0 {
		return ProjectileBoxCollision{}, fmt.Errorf("projectile collision size: %#v", input)
	}
	if input.TargetMinimum.X >= input.TargetMaximum.X ||
		input.TargetMinimum.Y >= input.TargetMaximum.Y ||
		input.TargetMinimum.Z >= input.TargetMaximum.Z {
		return ProjectileBoxCollision{}, fmt.Errorf("target bounds: %#v", input)
	}
	direction, err := normalizePosition(input.ProjectileDirection)
	if err != nil {
		return ProjectileBoxCollision{}, fmt.Errorf("directionNormalize: %w", err)
	}
	expandedMinimum := Position{
		X: input.TargetPosition.X + input.TargetMinimum.X - input.ProjectileHalfExtent.X,
		Y: input.TargetPosition.Y + input.TargetMinimum.Y - input.ProjectileHalfExtent.Y,
		Z: input.TargetPosition.Z + input.TargetMinimum.Z - input.ProjectileHalfExtent.Z,
	}
	expandedMaximum := Position{
		X: input.TargetPosition.X + input.TargetMaximum.X + input.ProjectileHalfExtent.X,
		Y: input.TargetPosition.Y + input.TargetMaximum.Y + input.ProjectileHalfExtent.Y,
		Z: input.TargetPosition.Z + input.TargetMaximum.Z + input.ProjectileHalfExtent.Z,
	}
	contactDistance, exitDistance, isContact := rayBoxContactDistance(
		input.ProjectilePosition, direction, expandedMinimum, expandedMaximum,
	)
	if isContact && contactDistance < input.MaximumDistance {
		return ProjectileBoxCollision{
			Position:           addPosition(input.ProjectilePosition, scalePosition(direction, contactDistance)),
			ExitPosition:       addPosition(input.ProjectilePosition, scalePosition(direction, exitDistance)),
			TravelDistance:     contactDistance,
			ExitTravelDistance: exitDistance,
			IsDirectHit:        true,
		}, nil
	}
	return ProjectileBoxCollision{
		Position:       addPosition(input.ProjectilePosition, scalePosition(direction, input.MaximumDistance)),
		TravelDistance: input.MaximumDistance,
	}, nil
}

func rayBoxContactDistance(
	origin Position, direction Position, minimum Position, maximum Position,
) (float32, float32, bool) {
	entry := float32(0)
	exit := float32(math.Inf(1))
	for _, axis := range [...]struct {
		origin    float32
		direction float32
		minimum   float32
		maximum   float32
	}{
		{origin.X, direction.X, minimum.X, maximum.X},
		{origin.Y, direction.Y, minimum.Y, maximum.Y},
		{origin.Z, direction.Z, minimum.Z, maximum.Z},
	} {
		if axis.direction == 0 {
			if axis.origin < axis.minimum || axis.origin > axis.maximum {
				return 0, 0, false
			}
			continue
		}
		first := (axis.minimum - axis.origin) / axis.direction
		second := (axis.maximum - axis.origin) / axis.direction
		if first > second {
			first, second = second, first
		}
		entry = max(entry, first)
		exit = min(exit, second)
		if entry > exit {
			return 0, 0, false
		}
	}
	if exit < 0 {
		return 0, 0, false
	}
	return entry, exit, true
}
