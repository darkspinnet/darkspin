package population

import (
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/sim"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

type ScatterRequest struct {
	Random    *sim.CapsuleRandom
	Origin    game.Vec3
	Mode      uint8
	Size      game.Vec3
	Footprint *game.NavigationFootprint
	Mesh      *navigation.Mesh
	Blockers  []game.BoundingBox
}

// ScatterPlacement follows build-103 sub_9FB180, retaining the exact origin
// if no candidate passes projection, reachability, and box overlap. Native
// draws share dword_145E1C8 with capsule selection; the server instead owns
// that stream per zone so concurrent games and checkpoint restores are stable.
func ScatterPlacement(req ScatterRequest) game.Vec3 {
	if req.Random == nil || req.Size.X <= 0 || req.Size.Y <= 0 || req.Size.Z <= 0 {
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
		x := float32(req.Random.Float64() * 10)
		y := float32(req.Random.Float64() * 10)
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
