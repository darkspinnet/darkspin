package sim

import (
	"errors"
	"fmt"
	"math"
)

// ResolveProjectileOrientedBoxCollision sweeps the authored projectile query
// box, with local Y along its facing, against a target's projected bounds.
// The 15 separating axes retain the box orientation rather than enlarging an
// axis-aligned approximation when the projectile travels diagonally.
func ResolveProjectileOrientedBoxCollision(req ProjectileBoxCollisionInput) (ProjectileBoxCollision, error) {
	if !isFinitePosition(req.ProjectilePosition) || !isFinitePosition(req.ProjectileHalfExtent) ||
		!isFinitePosition(req.TargetPosition) || !isFinitePosition(req.TargetMinimum) || !isFinitePosition(req.TargetMaximum) ||
		req.ProjectileHalfExtent.X <= 0 || req.ProjectileHalfExtent.Y <= 0 || req.ProjectileHalfExtent.Z <= 0 ||
		req.MaximumDistance < 0 || math.IsNaN(float64(req.MaximumDistance)) || math.IsInf(float64(req.MaximumDistance), 0) ||
		req.TargetMinimum.X > req.TargetMaximum.X || req.TargetMinimum.Y > req.TargetMaximum.Y || req.TargetMinimum.Z > req.TargetMaximum.Z {
		return ProjectileBoxCollision{}, errors.New("invalid oriented projectile query")
	}
	forward, err := normalizePosition(req.ProjectileDirection)
	if err != nil {
		return ProjectileBoxCollision{}, fmt.Errorf("direction: %w", err)
	}
	lateral := Position{X: -forward.Y, Y: forward.X}
	if lateral.X == 0 && lateral.Y == 0 {
		lateral.X = 1
	}
	lateral, err = normalizePosition(lateral)
	if err != nil {
		return ProjectileBoxCollision{}, fmt.Errorf("lateral: %w", err)
	}
	up := projectileCross(forward, lateral)
	projectileAxes := [3]Position{lateral, forward, up}
	worldAxes := [3]Position{{X: 1}, {Y: 1}, {Z: 1}}
	axes := make([]Position, 0, 15)
	axes = append(axes, projectileAxes[:]...)
	axes = append(axes, worldAxes[:]...)
	for _, projectileAxis := range projectileAxes {
		for _, worldAxis := range worldAxes {
			axes = append(axes, projectileCross(projectileAxis, worldAxis))
		}
	}
	targetHalfExtent := scalePosition(subtractPosition(req.TargetMaximum, req.TargetMinimum), .5)
	targetCenter := addPosition(req.TargetPosition, scalePosition(addPosition(req.TargetMinimum, req.TargetMaximum), .5))
	delta := subtractPosition(targetCenter, req.ProjectilePosition)
	entry := float32(0)
	exit := float32(math.Inf(1))
	for _, axis := range axes {
		if projectileDot(axis, axis) < 1e-12 {
			continue
		}
		radius := req.ProjectileHalfExtent.X*projectileAbs(projectileDot(lateral, axis)) +
			req.ProjectileHalfExtent.Y*projectileAbs(projectileDot(forward, axis)) +
			req.ProjectileHalfExtent.Z*projectileAbs(projectileDot(up, axis)) +
			targetHalfExtent.X*projectileAbs(axis.X) + targetHalfExtent.Y*projectileAbs(axis.Y) + targetHalfExtent.Z*projectileAbs(axis.Z)
		center := projectileDot(delta, axis)
		velocity := projectileDot(forward, axis)
		if projectileAbs(velocity) < 1e-7 {
			if projectileAbs(center) > radius {
				return ProjectileBoxCollision{}, nil
			}
			continue
		}
		near := (center - radius) / velocity
		far := (center + radius) / velocity
		if near > far {
			near, far = far, near
		}
		entry = max(entry, near)
		exit = min(exit, far)
		if entry > exit {
			return ProjectileBoxCollision{}, nil
		}
	}
	if exit < 0 || entry > req.MaximumDistance {
		return ProjectileBoxCollision{}, nil
	}
	return ProjectileBoxCollision{
		Position:       addPosition(req.ProjectilePosition, scalePosition(forward, entry)),
		ExitPosition:   addPosition(req.ProjectilePosition, scalePosition(forward, exit)),
		TravelDistance: entry, ExitTravelDistance: exit, IsDirectHit: true,
	}, nil
}

func projectileDot(first, second Position) float32 {
	return first.X*second.X + first.Y*second.Y + first.Z*second.Z
}

func projectileCross(first, second Position) Position {
	return Position{X: first.Y*second.Z - first.Z*second.Y, Y: first.Z*second.X - first.X*second.Z, Z: first.X*second.Y - first.Y*second.X}
}

func projectileAbs(component float32) float32 {
	return float32(math.Abs(float64(component)))
}
