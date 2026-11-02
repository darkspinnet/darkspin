package navigation

import "math"

// projectObstacleBoundary is enabled only on the selected Nocturna wall
// proxies. Their native collision can stop a hero slightly inside the server's
// expanded navigation clearance. All other obstacles keep strict rejection.
func (e *Mesh) projectObstacleBoundary(
	point Vec3, blocked Projection, options ProjectionOptions,
) (Projection, error) {
	terrain := *e
	terrain.obstacles = nil
	info := e.layers[options.PlanLayer].info
	boundaryOptions := options
	boundaryOptions.ComponentID = blocked.ComponentID
	boundaryOptions.IsComponentConstrained = true
	maximumSquared := options.MaxDistance * options.MaxDistance
	bestSquared := float32(math.Inf(1))
	bestHeight := float32(math.Inf(1))
	best := Projection{}
	// Use the same clearance margin as obstacle-route corners. Candidates
	// must project onto real terrain and clear every enabled collision box.
	padding := info.Radius + 0.05
	for _, obstacle := range e.obstacles {
		if !obstacle.IsBoundaryProjectionEnabled ||
			!obstacle.intersects(blocked.Position, blocked.Position, info.Radius, info.Height) {
			continue
		}
		local := obstacle.local(blocked.Position)
		candidates := [4]Vec3{
			{X: obstacle.Minimum.X - padding, Y: local.Y, Z: local.Z},
			{X: obstacle.Maximum.X + padding, Y: local.Y, Z: local.Z},
			{X: local.X, Y: obstacle.Minimum.Y - padding, Z: local.Z},
			{X: local.X, Y: obstacle.Maximum.Y + padding, Z: local.Z},
		}
		angle := float64(obstacle.Yaw) * math.Pi / 180
		cosine, sine := float32(math.Cos(angle)), float32(math.Sin(angle))
		for _, candidate := range candidates {
			world := Vec3{
				X: obstacle.Position.X + cosine*candidate.X - sine*candidate.Y,
				Y: obstacle.Position.Y + sine*candidate.X + cosine*candidate.Y,
				Z: obstacle.Position.Z + candidate.Z,
			}
			projection, err := terrain.Project(world, boundaryOptions)
			if err != nil {
				continue // No eligible terrain at this face within the query budget.
			}
			if e.isObstructed(projection.Position, options.PlanLayer) {
				continue
			}
			delta := subtract(point, projection.Position)
			height := float32(math.Abs(float64(delta.Z)))
			if options.IsPlanar {
				delta.Z = 0
			}
			squared := dot(delta, delta)
			if squared > maximumSquared || squared > bestSquared ||
				squared == bestSquared && (height > bestHeight ||
					height == bestHeight && projection.PolygonID >= best.PolygonID) {
				continue
			}
			bestSquared, bestHeight = squared, height
			projection.Distance = float32(math.Sqrt(float64(squared)))
			best = projection
		}
	}
	if math.IsInf(float64(bestSquared), 1) {
		return Projection{}, ErrProjectionObstructed
	}
	return best, nil
}
