package navigation

import "math"

// closestPlanarPointOnPolygon recovers the walkable height beneath a movement
// click. Orthogonal 3D projection would shift XY along a slope toward the
// actor's old height, or reject the click entirely on a long staircase.
func closestPlanarPointOnPolygon(point Vec3, polygon Polygon) (Vec3, bool) {
	flatPoint := Vec3{X: point.X, Y: point.Y}
	best := Vec3{}
	bestSquared := float32(math.Inf(1))
	for index := 1; index+1 < len(polygon.Edges); index++ {
		a := polygon.Edges[0].Vertex
		b := polygon.Edges[index].Vertex
		c := polygon.Edges[index+1].Vertex
		normal := cross(subtract(b, a), subtract(c, a))
		if math.Abs(float64(normal.Z)) <= 1e-8 {
			continue
		}
		candidate := closestPointOnTriangle(flatPoint,
			Vec3{X: a.X, Y: a.Y}, Vec3{X: b.X, Y: b.Y}, Vec3{X: c.X, Y: c.Y})
		candidateSquared := squaredDistance(flatPoint, candidate)
		if candidateSquared >= bestSquared {
			continue
		}
		candidate.Z = a.Z - (normal.X*(candidate.X-a.X)+normal.Y*(candidate.Y-a.Y))/normal.Z
		best = candidate
		bestSquared = candidateSquared
	}
	return best, !math.IsInf(float64(bestSquared), 1)
}
