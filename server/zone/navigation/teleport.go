package navigation

import (
	"errors"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	basenavigation "github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/sim"
)

const randomTeleportAttemptCount = 4

type ConnectedTeleportRequest struct {
	SourcePosition  game.Vec3
	FootprintRadius float32
	Radius          float32
	MinimumDistance float32
	AttemptCount    uint32
}

type RandomTeleportRequest struct {
	SourcePosition  game.Vec3
	FootprintRadius float32
	MinimumDistance float32
	NormalDistance  float32
	MaximumDistance float32
}

func ProjectPosition(
	mesh *basenavigation.Mesh, position game.Vec3, footprintRadius float32,
) (game.Vec3, bool, error) {
	if footprintRadius <= 0 {
		return game.Vec3{}, false, errors.New("position projection input invalid")
	}
	if mesh == nil {
		return position, false, nil
	}
	planLayer, isLayerFound := mesh.SelectLayer(footprintRadius, HeroHeight)
	if !isLayerFound {
		return position, false, nil
	}
	projection, err := mesh.Project(
		basenavigation.Vec3{X: position.X, Y: position.Y, Z: position.Z},
		basenavigation.ProjectionOptions{
			PlanLayer: planLayer, MaxDistance: ProjectionDistance,
		},
	)
	if err != nil {
		return position, false, nil
	}
	return game.Vec3{
		X: projection.Position.X,
		Y: projection.Position.Y,
		Z: projection.Position.Z,
	}, true, nil
}

func RandomTeleportDestination(
	mesh *basenavigation.Mesh,
	random *sim.SimulatorRandom,
	req RandomTeleportRequest,
) (game.Vec3, bool, error) {
	if random == nil || req.FootprintRadius <= 0 ||
		req.MinimumDistance <= 0 ||
		req.NormalDistance < req.MinimumDistance ||
		req.NormalDistance > req.MaximumDistance {
		return game.Vec3{}, false, errors.New("random teleport input invalid")
	}
	if mesh == nil {
		return req.SourcePosition, false, nil
	}
	planLayer, isLayerFound := mesh.SelectLayer(req.FootprintRadius, HeroHeight)
	if !isLayerFound {
		return req.SourcePosition, false, nil
	}
	start, err := mesh.Project(
		basenavigation.Vec3{
			X: req.SourcePosition.X,
			Y: req.SourcePosition.Y,
			Z: req.SourcePosition.Z,
		},
		basenavigation.ProjectionOptions{
			PlanLayer: planLayer, MaxDistance: ProjectionDistance,
		},
	)
	if err != nil {
		return req.SourcePosition, false, nil
	}
	options := basenavigation.ProjectionOptions{
		PlanLayer: planLayer, MaxDistance: ProjectionDistance,
		ComponentID: start.ComponentID, IsComponentConstrained: true,
	}
	for range randomTeleportAttemptCount {
		angle := random.Float64() * 2 * math.Pi
		candidate, candidateErr := mesh.Project(
			basenavigation.Vec3{
				X: req.SourcePosition.X + float32(math.Cos(angle))*req.NormalDistance,
				Y: req.SourcePosition.Y + float32(math.Sin(angle))*req.NormalDistance,
				Z: req.SourcePosition.Z,
			},
			options,
		)
		if candidateErr != nil || !mesh.IsReachable(start, candidate, planLayer) {
			continue
		}
		deltaX := candidate.Position.X - req.SourcePosition.X
		deltaY := candidate.Position.Y - req.SourcePosition.Y
		deltaZ := candidate.Position.Z - req.SourcePosition.Z
		distance := float32(math.Sqrt(float64(
			deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ,
		)))
		if distance < req.MinimumDistance || distance > req.MaximumDistance {
			continue
		}
		return game.Vec3{
			X: candidate.Position.X,
			Y: candidate.Position.Y,
			Z: candidate.Position.Z,
		}, true, nil
	}
	return req.SourcePosition, false, nil
}

func ConnectedTeleportDestination(
	mesh *basenavigation.Mesh,
	random *sim.SimulatorRandom,
	req ConnectedTeleportRequest,
) (game.Vec3, bool, error) {
	if random == nil || req.FootprintRadius <= 0 || req.Radius <= 0 ||
		req.MinimumDistance < 0 || req.AttemptCount == 0 {
		return game.Vec3{}, false, errors.New("connected teleport input invalid")
	}
	if mesh == nil {
		return req.SourcePosition, false, nil
	}
	planLayer, isLayerFound := mesh.SelectLayer(req.FootprintRadius, HeroHeight)
	if !isLayerFound {
		return req.SourcePosition, false, nil
	}
	start, err := mesh.Project(
		basenavigation.Vec3{
			X: req.SourcePosition.X,
			Y: req.SourcePosition.Y,
			Z: req.SourcePosition.Z,
		},
		basenavigation.ProjectionOptions{
			PlanLayer: planLayer, MaxDistance: ProjectionDistance,
		},
	)
	if err != nil {
		return req.SourcePosition, false, nil
	}
	options := basenavigation.ProjectionOptions{
		PlanLayer: planLayer, MaxDistance: ProjectionDistance,
		ComponentID: start.ComponentID, IsComponentConstrained: true,
	}
	for range req.AttemptCount {
		angle := random.Float64() * 2 * math.Pi
		candidate := game.Vec3{
			X: req.SourcePosition.X + float32(math.Cos(angle))*req.Radius,
			Y: req.SourcePosition.Y + float32(math.Sin(angle))*req.Radius,
			Z: req.SourcePosition.Z,
		}
		projection, projectionErr := mesh.Project(
			basenavigation.Vec3{
				X: candidate.X, Y: candidate.Y, Z: candidate.Z,
			},
			options,
		)
		if projectionErr != nil || !mesh.IsReachable(start, projection, planLayer) {
			continue
		}
		delta := candidate.Sub(req.SourcePosition)
		if delta.Length() <= req.MinimumDistance {
			continue
		}
		return candidate, true, nil
	}
	return req.SourcePosition, false, nil
}

func DirectMovementDestination(
	mesh *basenavigation.Mesh,
	source game.Vec3,
	desired game.Vec3,
	footprintRadius float32,
) (game.Vec3, bool, error) {
	if footprintRadius <= 0 {
		return game.Vec3{}, false, errors.New("direct movement input invalid")
	}
	if mesh == nil {
		return source, false, nil
	}
	planLayer, isLayerFound := mesh.SelectLayer(footprintRadius, HeroHeight)
	if !isLayerFound {
		return source, false, nil
	}
	path, err := mesh.CreatePath(
		basenavigation.Vec3{X: source.X, Y: source.Y, Z: source.Z},
		basenavigation.Vec3{X: desired.X, Y: desired.Y, Z: desired.Z},
		basenavigation.PathOptions{
			ProjectionOptions: basenavigation.ProjectionOptions{
				PlanLayer: planLayer, MaxDistance: ProjectionDistance,
			},
			MaxVisitedPolygon: 4096,
		},
	)
	if err != nil || len(path.Points) != 2 {
		return source, false, nil
	}
	return game.Vec3{
		X: path.Goal.Position.X,
		Y: path.Goal.Position.Y,
		Z: path.Goal.Position.Z,
	}, true, nil
}

func ReachableTeleportDestination(
	mesh *basenavigation.Mesh,
	source game.Vec3,
	desired game.Vec3,
	footprintRadius float32,
) (game.Vec3, bool, error) {
	if footprintRadius <= 0 {
		return game.Vec3{}, false, errors.New("reachable teleport input invalid")
	}
	if mesh == nil {
		return source, false, nil
	}
	planLayer, isLayerFound := mesh.SelectLayer(footprintRadius, HeroHeight)
	if !isLayerFound {
		return source, false, nil
	}
	path, err := mesh.CreatePath(
		basenavigation.Vec3{X: source.X, Y: source.Y, Z: source.Z},
		basenavigation.Vec3{X: desired.X, Y: desired.Y, Z: desired.Z},
		basenavigation.PathOptions{
			ProjectionOptions: basenavigation.ProjectionOptions{
				PlanLayer: planLayer, MaxDistance: ProjectionDistance,
			},
			MaxVisitedPolygon: 4096,
		},
	)
	if err != nil {
		return source, false, nil
	}
	return game.Vec3{
		X: path.Goal.Position.X,
		Y: path.Goal.Position.Y,
		Z: path.Goal.Position.Z,
	}, true, nil
}
