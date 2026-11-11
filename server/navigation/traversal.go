package navigation

import (
	"errors"
	"fmt"
	"math"
	"sync/atomic"
)

// TraversalLink is an ordered authored trigger-to-destination transition.
// It is separate from fence collision geometry and never supplies a walk edge.
type TraversalLink struct {
	MarkerID      uint32
	Source        Vec3
	Destination   Vec3
	Rotation      Vec3
	Offset        Vec3
	BoxDimensions Vec3
	SphereRadius  float32
	IsSphere      bool
	isEnabled     *atomic.Bool
}

// Traversal marks the zero-distance jump after Points[PointIndex].
type Traversal struct {
	IsEnabled   bool
	MarkerID    uint32
	PointIndex  int
	Source      Vec3
	Destination Vec3
}

func (e *Mesh) WithTraversalLinks(links []TraversalLink) (*Mesh, error) {
	if e == nil {
		return nil, errors.New("traversal mesh unavailable")
	}
	result := *e
	if result.topologyRevision == nil {
		result.topologyRevision = new(atomic.Uint64)
	}
	result.links = append([]TraversalLink(nil), links...)
	for index := range result.links {
		link := &result.links[index]
		if link.MarkerID == 0 || !isFinite(link.Source) || !isFinite(link.Destination) ||
			!isFinite(link.Rotation) || !isFinite(link.Offset) || !isFinite(link.BoxDimensions) ||
			!isFiniteScalar(link.SphereRadius) || link.IsSphere && link.SphereRadius <= 0 ||
			!link.IsSphere && (link.BoxDimensions.X <= 0 || link.BoxDimensions.Y <= 0 || link.BoxDimensions.Z <= 0) {
			return nil, fmt.Errorf("traversalBounds[%d]: invalid", index)
		}
		link.isEnabled = new(atomic.Bool)
	}
	return &result, nil
}

func (e *Mesh) SetTraversalLinkEnabled(markerID uint32, isEnabled bool) {
	if e == nil {
		return
	}
	for _, link := range e.links {
		if link.MarkerID == markerID && link.isEnabled.Swap(isEnabled) != isEnabled {
			e.topologyRevision.Add(1)
		}
	}
}

func (e *Mesh) TopologyRevision() uint64 {
	if e == nil || e.topologyRevision == nil {
		return 0
	}
	return e.topologyRevision.Load()
}

// CreatePath permits authored jumps only for callers that explicitly consume
// Traversals. Forced and direct movement retain walk-only paths by default.
func (e *Mesh) CreatePath(start Vec3, goal Vec3, options PathOptions) (Path, error) {
	best, walkErr := e.walkingPath(start, goal, options)
	if !options.IsTraversalAllowed || e == nil {
		if walkErr != nil {
			return Path{}, fmt.Errorf("createWalk: %w", walkErr)
		}
		return best, nil
	}
	bestDistance := float32(math.Inf(1))
	if walkErr == nil {
		bestDistance = best.Distance()
	}
	for _, link := range e.links {
		if !link.isEnabled.Load() {
			continue
		}
		// A route through this gate must walk to its trigger and from its
		// destination. Skip expensive obstacle searches when even those
		// straight-line lower bounds cannot improve the current route.
		center := add(link.Source, link.Offset)
		triggerRadius := link.SphereRadius
		if !link.IsSphere {
			triggerRadius = distance(Vec3{}, link.BoxDimensions) / 2
		}
		approachMinimum := max(float32(0), planarDistance(start, center)-triggerRadius-options.MaxDistance)
		departureMinimum := max(float32(0), planarDistance(link.Destination, goal)-2*options.MaxDistance)
		if approachMinimum+departureMinimum > bestDistance {
			continue
		}
		entry := link.approach(start)
		approach, err := e.walkingPath(start, entry, options)
		if err != nil {
			continue // An authored source must have a real unobstructed approach.
		}
		if !link.contains(approach.Goal.Position) {
			continue
		}
		approachDistance := approach.Distance()
		if approachDistance+departureMinimum >= bestDistance {
			continue
		}
		departureOptions := options
		departureOptions.IsComponentConstrained = false
		departure, err := e.walkingPath(link.Destination, goal, departureOptions)
		if err != nil {
			continue
		}
		travelDistance := approachDistance + departure.Distance()
		if travelDistance >= bestDistance {
			continue
		}
		points := append([]Vec3(nil), approach.Points...)
		entryIndex := len(points) - 1
		points = append(points, link.Destination)
		points = append(points, departure.Points...)
		best = Path{
			Start: approach.Start, Goal: departure.Goal,
			Corridor: append(append([]uint32(nil), approach.Corridor...), departure.Corridor...),
			Points:   points, Traversals: []Traversal{{MarkerID: link.MarkerID, PointIndex: entryIndex,
				Source: approach.Goal.Position, Destination: link.Destination, IsEnabled: true}},
		}
		bestDistance = travelDistance
	}
	if math.IsInf(float64(bestDistance), 1) {
		return Path{}, fmt.Errorf("createRoute: %w", walkErr)
	}
	return best, nil
}

func (e Path) Distance() float32 {
	total := float32(0)
	for index := 1; index < len(e.Points); index++ {
		if e.TraversalAt(index-1).MarkerID == 0 {
			total += distance(e.Points[index-1], e.Points[index])
		}
	}
	return total
}

func (e Path) TraversalAt(pointIndex int) Traversal {
	for _, traversal := range e.Traversals {
		if traversal.PointIndex == pointIndex {
			return traversal
		}
	}
	return Traversal{}
}

// TraversalSegment recognizes only an ordered jump ending at the selected
// destination. Companion path adapters preserve these exact endpoint pairs.
func (e *Mesh) TraversalSegment(start Vec3, end Vec3) Traversal {
	if e == nil {
		return Traversal{}
	}
	for _, link := range e.links {
		if end == link.Destination && link.contains(start) {
			return Traversal{MarkerID: link.MarkerID, Source: start, Destination: end, IsEnabled: link.isEnabled.Load()}
		}
	}
	return Traversal{}
}

// TraverseSegment evaluates trigger entry using authored geometry, independently
// of path planning. Actors already occupying the trigger do not re-enter it.
func (e *Mesh) TraverseSegment(start Vec3, end Vec3) Traversal {
	if e == nil {
		return Traversal{}
	}
	bestFraction := float32(math.Inf(1))
	best := Traversal{}
	for _, link := range e.links {
		if !link.isEnabled.Load() || link.contains(start) {
			continue
		}
		fraction, isCrossed := link.contactFraction(start, end)
		if isCrossed && fraction < bestFraction {
			bestFraction = fraction
			best = Traversal{MarkerID: link.MarkerID, Source: add(start, scale(subtract(end, start), fraction)), Destination: link.Destination, IsEnabled: true}
		}
	}
	return best
}

func (e TraversalLink) local(point Vec3) Vec3 {
	local := subtract(subtract(point, e.Source), e.Offset)
	if e.IsSphere {
		return local
	}
	sx, cx := math.Sincos(float64(e.Rotation.X) * math.Pi / 180)
	sy, cy := math.Sincos(float64(e.Rotation.Y) * math.Pi / 180)
	sz, cz := math.Sincos(float64(e.Rotation.Z) * math.Pi / 180)
	x := float64(local.X)*cz + float64(local.Y)*sz
	y := -float64(local.X)*sz + float64(local.Y)*cz
	z := float64(local.Z)
	x, z = x*cy-z*sy, x*sy+z*cy
	y, z = y*cx+z*sx, -y*sx+z*cx
	return Vec3{X: float32(x), Y: float32(y), Z: float32(z)}
}

func (e TraversalLink) contains(point Vec3) bool {
	local := e.local(point)
	if e.IsSphere {
		return dot(local, local) <= e.SphereRadius*e.SphereRadius
	}
	return float32(math.Abs(float64(local.X))) <= e.BoxDimensions.X/2 &&
		float32(math.Abs(float64(local.Y))) <= e.BoxDimensions.Y/2 &&
		float32(math.Abs(float64(local.Z))) <= e.BoxDimensions.Z/2
}

func (e TraversalLink) contactFraction(start Vec3, end Vec3) (float32, bool) {
	start, end = e.local(start), e.local(end)
	delta := subtract(end, start)
	if e.IsSphere {
		a := dot(delta, delta)
		if a == 0 {
			return 0, e.contains(add(e.Source, add(e.Offset, start)))
		}
		b := dot(start, delta)
		c := dot(start, start) - e.SphereRadius*e.SphereRadius
		discriminant := b*b - a*c
		if discriminant < 0 {
			return 0, false
		}
		fraction := (-b - float32(math.Sqrt(float64(discriminant)))) / a
		return max(float32(0), fraction), fraction <= 1 && (-b+float32(math.Sqrt(float64(discriminant))))/a >= 0
	}
	minimum, maximum := float32(0), float32(1)
	axes := [3][3]float32{{start.X, delta.X, e.BoxDimensions.X / 2}, {start.Y, delta.Y, e.BoxDimensions.Y / 2}, {start.Z, delta.Z, e.BoxDimensions.Z / 2}}
	for _, axis := range axes {
		if axis[1] == 0 {
			if axis[0] < -axis[2] || axis[0] > axis[2] {
				return 0, false
			}
			continue
		}
		first, last := (-axis[2]-axis[0])/axis[1], (axis[2]-axis[0])/axis[1]
		minimum = max(minimum, min(first, last))
		maximum = min(maximum, max(first, last))
		if minimum > maximum {
			return 0, false
		}
	}
	return minimum, true
}

func (e TraversalLink) approach(start Vec3) Vec3 {
	if e.contains(start) {
		return start
	}
	center := add(e.Source, e.Offset)
	// Keep the actor's terrain height while approaching the upright trigger.
	center.Z = start.Z
	fraction, isCrossed := e.contactFraction(start, center)
	if !isCrossed {
		return center
	}
	// Round toward the trigger's interior without inventing a contact margin.
	fraction = math.Nextafter32(fraction, 1)
	return add(start, scale(subtract(center, start), fraction))
}
