package navigation

import (
	"errors"
	"fmt"
	"math"
)

// ProjectInteractionTarget finds a walkable contact point on the actor's side
// of a fixed object's collider. The object's center is not a movement goal.
// Terrain connectivity and all collision blockers remain in force at contact.
func (e *Mesh) ProjectInteractionTarget(
	start Projection, target Vec3, options ProjectionOptions,
) (Projection, error) {
	projection, err := e.projectContactTarget(start, target, options, true)
	if err != nil {
		return Projection{}, fmt.Errorf("interactionContact: %w", err)
	}
	return projection, nil
}

// ProjectMovementTarget stops a blocked movement click at the near collider
// edge. MaxDistance still bounds terrain projection, but does not bound the
// distance from the click to that edge: a walk command can point deep inside
// a large collider without making the reachable approach invalid.
func (e *Mesh) ProjectMovementTarget(
	start Projection, target Vec3, options ProjectionOptions,
) (Projection, error) {
	projection, err := e.projectContactTarget(start, target, options, false)
	if err != nil {
		return Projection{}, fmt.Errorf("movementContact: %w", err)
	}
	return projection, nil
}

func (e *Mesh) projectContactTarget(
	start Projection, target Vec3, options ProjectionOptions, isContactDistanceConstrained bool,
) (Projection, error) {
	if e == nil || !isFinite(start.Position) {
		return Projection{}, errors.New("interaction projection invalid")
	}
	terrain := *e
	terrain.obstacles = nil
	targetProjection, err := terrain.Project(target, options)
	if err != nil {
		return Projection{}, fmt.Errorf("contactTerrain: %w", err)
	}
	info := e.layers[options.PlanLayer].info
	contactFraction := float32(1)
	for _, obstacle := range e.obstacles {
		fraction, isContactBlocked := obstacle.interactionContactFraction(
			start.Position, targetProjection.Position, info.Radius, info.Height,
		)
		if isContactBlocked {
			contactFraction = min(contactFraction, fraction)
		}
	}
	if contactFraction == 1 {
		return e.Project(target, options)
	}
	delta := subtract(targetProjection.Position, start.Position)
	distance := float32(math.Sqrt(float64(squaredDistance(start.Position, targetProjection.Position))))
	if distance == 0 {
		return Projection{}, errors.New("interaction contact obstructed")
	}
	// Keep the actor just outside the expanded box after terrain projection.
	contactFraction = max(float32(0), contactFraction-0.05/distance)
	contact := add(start.Position, scale(delta, contactFraction))
	projection, err := e.Project(contact, options)
	if err != nil {
		return Projection{}, fmt.Errorf("contactProject: %w", err)
	}
	contactTarget := target
	if options.IsPlanar {
		// The movement click's Z is only a surface-selection hint. Measure
		// contact clearance from the recovered terrain point instead.
		contactTarget = targetProjection.Position
	}
	if isContactDistanceConstrained &&
		squaredDistance(projection.Position, contactTarget) > options.MaxDistance*options.MaxDistance {
		return Projection{}, errors.New("interaction contact too distant")
	}
	return projection, nil
}

func (e Obstacle) interactionContactFraction(
	start Vec3, target Vec3, radius float32, height float32,
) (float32, bool) {
	if !e.intersects(target, target, radius, height) {
		return 1, false
	}
	start, target = e.local(start), e.local(target)
	minimums := [3]float32{e.Minimum.X - radius, e.Minimum.Y - radius, e.Minimum.Z - height}
	maximums := [3]float32{e.Maximum.X + radius, e.Maximum.Y + radius, e.Maximum.Z}
	starts := [3]float32{start.X, start.Y, start.Z}
	targets := [3]float32{target.X, target.Y, target.Z}
	entry := float32(0)
	for axis := range starts {
		delta := targets[axis] - starts[axis]
		if math.Abs(float64(delta)) < 0.000001 {
			continue
		}
		first := (minimums[axis] - starts[axis]) / delta
		last := (maximums[axis] - starts[axis]) / delta
		entry = max(entry, min(first, last))
	}
	return entry, true
}
