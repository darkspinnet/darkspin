package navigation

import "math"

type pathPortal struct {
	start Vec3
	end   Vec3
}

// straightenPath skips unnecessary portal-midpoint turns only when the segment
// crosses every intervening portal in order. Crossing heights remain on the
// authored mesh, including slope changes. Convex polygons make each segment
// between successive crossings traversable within the selected corridor.
func straightenPath(points []Vec3, portals []pathPortal) []Vec3 {
	if len(points) < 3 || len(portals) != len(points)-2 {
		return points
	}
	results := make([]Vec3, 0, len(points))
	results = append(results, points[0])
	for anchor := 0; anchor+1 < len(points); {
		next := anchor + 1
		for candidate := len(points) - 1; candidate > next; candidate-- {
			if isCorridorSegment(points[anchor], points[candidate], portals[anchor:candidate-1]) {
				next = candidate
				break
			}
		}
		for _, portal := range portals[anchor : next-1] {
			crossing, isCrossed := pathPortalCrossing(points[anchor], points[next], portal)
			if isCrossed {
				results = append(results, crossing)
			}
		}
		results = append(results, points[next])
		anchor = next
	}
	return results
}

func isCorridorSegment(start Vec3, end Vec3, portals []pathPortal) bool {
	previousDistance := float32(0)
	for _, portal := range portals {
		crossing, isCrossed := pathPortalCrossing(start, end, portal)
		if !isCrossed {
			return false
		}
		deltaX, deltaY := crossing.X-start.X, crossing.Y-start.Y
		distanceSquared := deltaX*deltaX + deltaY*deltaY
		if distanceSquared+bfxTurnEpsilon < previousDistance {
			return false
		}
		previousDistance = distanceSquared
	}
	return true
}

func pathPortalCrossing(start Vec3, end Vec3, portal pathPortal) (Vec3, bool) {
	deltaX, deltaY := float64(end.X-start.X), float64(end.Y-start.Y)
	edgeX, edgeY := float64(portal.end.X-portal.start.X), float64(portal.end.Y-portal.start.Y)
	offsetX, offsetY := float64(portal.start.X-start.X), float64(portal.start.Y-start.Y)
	denominator := deltaX*edgeY - deltaY*edgeX
	if math.Abs(denominator) <= float64(bfxTurnEpsilon) {
		// Collinear/degenerate portals keep their existing midpoint route.
		return Vec3{}, false
	}
	segmentPortion := (offsetX*edgeY - offsetY*edgeX) / denominator
	edgePortion := (offsetX*deltaY - offsetY*deltaX) / denominator
	const tolerance = 0.000001
	if segmentPortion < -tolerance || segmentPortion > 1+tolerance ||
		edgePortion < -tolerance || edgePortion > 1+tolerance {
		return Vec3{}, false
	}
	return add(portal.start, scale(subtract(portal.end, portal.start), float32(max(0, min(1, edgePortion))))), true
}
