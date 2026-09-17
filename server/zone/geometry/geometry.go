package geometry

import (
	"math"

	"github.com/darkspinnet/darkspin/server/game"
)

func IsFinite(position game.Vec3) bool {
	return IsFiniteScalar(position.X) && IsFiniteScalar(position.Y) &&
		IsFiniteScalar(position.Z)
}

func IsFiniteScalar(scalar float32) bool {
	return !math.IsNaN(float64(scalar)) && !math.IsInf(float64(scalar), 0)
}

func IsFinitePositiveScalar(scalar float32) bool {
	return scalar > 0 && IsFiniteScalar(scalar)
}

func IsReported(position game.Vec3) bool {
	return IsFinite(position) &&
		(position.X != 0 || position.Y != 0 || position.Z != 0)
}

func DistanceSquared(first game.Vec3, second game.Vec3) float32 {
	return distanceSquared(first, second)
}

func Distance(first game.Vec3, second game.Vec3) float32 {
	return float32(math.Sqrt(float64(distanceSquared(first, second))))
}

func Direction(start game.Vec3, end game.Vec3) game.Vec3 {
	direction := subtract(end, start)
	lengthSquared := dot(direction, direction)
	if lengthSquared == 0 {
		return game.Vec3{X: 1}
	}
	inverseLength := 1 / float32(math.Sqrt(float64(lengthSquared)))
	return scale(direction, inverseLength)
}

func ContainsSphere(
	position game.Vec3, center game.Vec3, radius float32,
) bool {
	if radius <= 0 || !IsFinite(position) || !IsFinite(center) {
		return false
	}
	return distanceSquared(position, center) <= radius*radius
}

func ContainsSphereStrict(
	position game.Vec3, center game.Vec3, radius float32,
) bool {
	if radius <= 0 || !IsFinite(position) || !IsFinite(center) {
		return false
	}
	return distanceSquared(position, center) < radius*radius
}

func IsDestinationInRange(
	start game.Vec3, end game.Vec3, maximumDistance float32,
) bool {
	if maximumDistance <= 0 || !IsFinite(start) || !IsFinite(end) {
		return false
	}
	return distanceSquared(start, end) <= maximumDistance*maximumDistance
}

func SegmentIntersectsSphere(
	start game.Vec3, end game.Vec3, center game.Vec3, radius float32,
) bool {
	if radius <= 0 || !IsFinite(start) || !IsFinite(end) ||
		!IsFinite(center) {
		return false
	}
	delta := subtract(end, start)
	lengthSquared := dot(delta, delta)
	if lengthSquared == 0 {
		return ContainsSphere(start, center, radius)
	}
	fraction := dot(subtract(center, start), delta) / lengthSquared
	fraction = max(float32(0), min(float32(1), fraction))
	closest := add(start, scale(delta, fraction))
	return ContainsSphere(closest, center, radius)
}

func SegmentSphereEntryFraction(
	start game.Vec3, end game.Vec3, center game.Vec3, radius float32,
) (float32, bool) {
	if radius <= 0 || !IsFinite(start) || !IsFinite(end) ||
		!IsFinite(center) {
		return 0, false
	}
	delta := subtract(end, start)
	offset := subtract(start, center)
	a := dot(delta, delta)
	if a == 0 {
		return 0, ContainsSphere(start, center, radius)
	}
	c := dot(offset, offset) - radius*radius
	if c <= 0 {
		return 0, true
	}
	b := 2 * dot(offset, delta)
	discriminant := b*b - 4*a*c
	if discriminant < 0 {
		return 0, false
	}
	fraction := (-b - float32(math.Sqrt(float64(discriminant)))) / (2 * a)
	if fraction < 0 || fraction > 1 {
		return 0, false
	}
	return fraction, true
}

func SegmentIntersectsBox(
	start game.Vec3,
	end game.Vec3,
	center game.Vec3,
	halfExtent game.Vec3,
) bool {
	_, isIntersected := SegmentBoxEntryFraction(
		start,
		end,
		center,
		halfExtent,
	)
	return isIntersected
}

func SegmentBoxEntryFraction(
	start game.Vec3,
	end game.Vec3,
	center game.Vec3,
	halfExtent game.Vec3,
) (float32, bool) {
	if !IsFinite(start) || !IsFinite(end) || !IsFinite(center) ||
		!IsFinitePositiveScalar(halfExtent.X) ||
		!IsFinitePositiveScalar(halfExtent.Y) ||
		!IsFinitePositiveScalar(halfExtent.Z) {
		return 0, false
	}
	entry := float32(0)
	exit := float32(1)
	axis := [][4]float32{
		{
			start.X, end.X - start.X,
			center.X - halfExtent.X, center.X + halfExtent.X,
		},
		{
			start.Y, end.Y - start.Y,
			center.Y - halfExtent.Y, center.Y + halfExtent.Y,
		},
		{
			start.Z, end.Z - start.Z,
			center.Z - halfExtent.Z, center.Z + halfExtent.Z,
		},
	}
	for _, component := range axis {
		if math.Abs(float64(component[1])) < 0.000001 {
			if component[0] < component[2] ||
				component[0] > component[3] {
				return 0, false
			}
			continue
		}
		first := (component[2] - component[0]) / component[1]
		second := (component[3] - component[0]) / component[1]
		if first > second {
			first, second = second, first
		}
		entry = max(entry, first)
		exit = min(exit, second)
		if entry > exit {
			return 0, false
		}
	}
	return entry, entry >= 0 && entry <= 1
}

func distanceSquared(first game.Vec3, second game.Vec3) float32 {
	return dot(subtract(first, second), subtract(first, second))
}

func subtract(first game.Vec3, second game.Vec3) game.Vec3 {
	return game.Vec3{
		X: first.X - second.X,
		Y: first.Y - second.Y,
		Z: first.Z - second.Z,
	}
}

func add(first game.Vec3, second game.Vec3) game.Vec3 {
	return game.Vec3{
		X: first.X + second.X,
		Y: first.Y + second.Y,
		Z: first.Z + second.Z,
	}
}

func scale(position game.Vec3, scalar float32) game.Vec3 {
	return game.Vec3{
		X: position.X * scalar,
		Y: position.Y * scalar,
		Z: position.Z * scalar,
	}
}

func dot(first game.Vec3, second game.Vec3) float32 {
	return first.X*second.X + first.Y*second.Y + first.Z*second.Z
}
