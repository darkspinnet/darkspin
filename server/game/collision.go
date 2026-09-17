package game

import "math"

// Vec3 is the server-side world vector.
type Vec3 struct{ X, Y, Z float32 }

func (a Vec3) Add(b Vec3) Vec3          { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func (a Vec3) Sub(b Vec3) Vec3          { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func (a Vec3) Scale(value float32) Vec3 { return Vec3{a.X * value, a.Y * value, a.Z * value} }
func (a Vec3) LengthSquared() float32   { return a.X*a.X + a.Y*a.Y + a.Z*a.Z }
func (a Vec3) Length() float32          { return float32(math.Sqrt(float64(a.LengthSquared()))) }

type BoundingBox struct{ Center, Extent Vec3 }

func NewBoundingBox(minimum, maximum Vec3) BoundingBox {
	return BoundingBox{Center: maximum.Add(minimum).Scale(0.5), Extent: maximum.Sub(minimum).Scale(0.5)}
}

func (b BoundingBox) Minimum() Vec3 { return b.Center.Sub(b.Extent) }
func (b BoundingBox) Maximum() Vec3 { return b.Center.Add(b.Extent) }
func (b BoundingBox) Size() Vec3    { return b.Extent.Scale(2) }
func (b BoundingBox) IsPoint() bool { return b.Extent.X == 0 || b.Extent.Y == 0 || b.Extent.Z == 0 }

func (b BoundingBox) ContainsPoint(point Vec3) bool {
	distance := absVec(b.Center.Sub(point))
	return distance.X < b.Extent.X && distance.Y < b.Extent.Y && distance.Z < b.Extent.Z
}

func (b BoundingBox) ContainsBox(other BoundingBox) bool {
	distance := absVec(b.Center.Sub(other.Center))
	required := b.Extent.Sub(other.Extent)
	return distance.X <= required.X && distance.Y <= required.Y && distance.Z <= required.Z
}

func (b BoundingBox) IntersectsBox(other BoundingBox) bool {
	distance := absVec(b.Center.Sub(other.Center))
	required := b.Extent.Add(other.Extent)
	return distance.X <= required.X && distance.Y <= required.Y && distance.Z <= required.Z
}

func (b BoundingBox) IntersectsSphere(sphere BoundingSphere) bool { return sphere.IntersectsBox(b) }

type BoundingSphere struct {
	Center Vec3
	Radius float32
}

func (s BoundingSphere) IsPoint() bool { return s.Radius == 0 }
func (s BoundingSphere) ContainsPoint(point Vec3) bool {
	return s.Center.Sub(point).Length() < s.Radius
}
func (s BoundingSphere) ContainsSphere(other BoundingSphere) bool {
	radius := s.Radius + other.Radius
	return s.Center.Sub(other.Center).LengthSquared() < radius*radius
}
func (s BoundingSphere) IntersectsSphere(other BoundingSphere) bool {
	radius := s.Radius + other.Radius
	return s.Center.Sub(other.Center).LengthSquared() <= radius*radius
}
func (s BoundingSphere) IntersectsBox(box BoundingBox) bool {
	closest := clampVec(s.Center, box.Minimum(), box.Maximum())
	return closest.Sub(s.Center).Length() <= s.Radius
}

type BoundingCapsule struct {
	Center             Vec3
	Radius, HalfHeight float32
}

func NewBoundingCapsule(center Vec3, radius, height float32) BoundingCapsule {
	return BoundingCapsule{Center: center, Radius: radius, HalfHeight: height * 0.5}
}
func (c BoundingCapsule) Minimum() Vec3 { value := c.Center; value.Y -= c.HalfHeight; return value }
func (c BoundingCapsule) Maximum() Vec3 { value := c.Center; value.Y += c.HalfHeight; return value }
func (c BoundingCapsule) ContainsPoint(point Vec3) bool {
	bottom := c.Center.Y - c.HalfHeight + c.Radius
	top := c.Center.Y + c.HalfHeight - c.Radius
	closestY := min(max(point.Y, bottom), top)
	closest := Vec3{X: c.Center.X, Y: closestY, Z: c.Center.Z}
	return point.Sub(closest).LengthSquared() < c.Radius*c.Radius
}
func (c BoundingCapsule) IntersectsSphere(sphere BoundingSphere) bool {
	bottom := c.Center.Y - c.HalfHeight + c.Radius
	top := c.Center.Y + c.HalfHeight - c.Radius
	closestY := min(max(sphere.Center.Y, bottom), top)
	closest := Vec3{X: c.Center.X, Y: closestY, Z: c.Center.Z}
	radius := c.Radius + sphere.Radius
	return sphere.Center.Sub(closest).LengthSquared() <= radius*radius
}

func absVec(value Vec3) Vec3 {
	return Vec3{float32(math.Abs(float64(value.X))), float32(math.Abs(float64(value.Y))), float32(math.Abs(float64(value.Z)))}
}
func clampVec(value, minimum, maximum Vec3) Vec3 {
	return Vec3{min(max(value.X, minimum.X), maximum.X), min(max(value.Y, minimum.Y), maximum.Y), min(max(value.Z, minimum.Z), maximum.Z)}
}
