package population

import (
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

// Native scatter uses process-global dword_1465600, separately from the map
// layout state and simulator MT. Initialize from the clock in place of RDTSC.
var portalScatterRandom scatterRandom

type scatterRandom struct {
	mu    sync.Mutex
	state uint32
}

func (e *scatterRandom) draw() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state == 0 {
		e.state = uint32(time.Now().UnixNano())
		if e.state == 0 {
			e.state = 1
		}
	}
	e.state *= 663608941
	return float64(int32(e.state))*(1.0/4294967296.0) + 0.5
}

type ScatterRequest struct {
	Origin    game.Vec3
	Mode      uint8
	Size      game.Vec3
	Footprint *game.NavigationFootprint
	Mesh      *navigation.Mesh
	Blockers  []game.BoundingBox
}

// ScatterPlacement follows sub_9F9150 -> sub_9F5850, retaining the exact
// origin if no candidate passes projection, reachability, and box overlap.
func ScatterPlacement(req ScatterRequest) game.Vec3 {
	if req.Size.X <= 0 || req.Size.Y <= 0 || req.Size.Z <= 0 {
		return req.Origin
	}
	var originProjection navigation.Projection
	var options navigation.ProjectionOptions
	isNavigationReady := false
	if req.Mesh != nil {
		radius := req.Size.X * 0.5
		if req.Footprint != nil {
			var radiusErr error
			radius, radiusErr = req.Footprint.SpawnRadius()
			if radiusErr != nil {
				return req.Origin
			}
		}
		planLayer, isLayerFound := req.Mesh.SelectFootprintLayer(radius, req.Mode)
		if isLayerFound {
			options = navigation.ProjectionOptions{
				PlanLayer: planLayer, MaxDistance: zonenavigation.ProjectionDistance,
			}
			projection, err := req.Mesh.Project(navigation.Vec3{
				X: req.Origin.X, Y: req.Origin.Y, Z: req.Origin.Z,
			}, options)
			if err == nil {
				originProjection = projection
				isNavigationReady = true
			}
		}
	}
	for attempt := 0; attempt < 8; attempt++ {
		x := float32(portalScatterRandom.draw() * 10)
		y := float32(portalScatterRandom.draw() * 10)
		position := game.Vec3{X: req.Origin.X + x, Y: req.Origin.Y + y, Z: req.Origin.Z}
		if !isNavigationReady {
			continue
		}
		projection, err := req.Mesh.Project(navigation.Vec3{
			X: position.X, Y: position.Y, Z: position.Z,
		}, options)
		if err != nil || !req.Mesh.IsReachable(originProjection, projection, options.PlanLayer) {
			continue
		}
		position = game.Vec3{X: projection.Position.X, Y: projection.Position.Y, Z: projection.Position.Z}
		box := game.BoundingBox{
			Center: position.Add(game.Vec3{Z: req.Size.Z * 0.5}), Extent: req.Size.Scale(0.4),
		}
		if !isScatterBlocked(box, req.Blockers) {
			return position
		}
	}
	return req.Origin
}

func isScatterBlocked(box game.BoundingBox, blockers []game.BoundingBox) bool {
	for _, blocker := range blockers {
		if box.IntersectsBox(blocker) {
			return true
		}
	}
	return false
}
