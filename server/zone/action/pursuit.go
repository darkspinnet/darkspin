package action

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	basenavigation "github.com/darkspinnet/darkspin/server/navigation"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

const maximumVisitedPolygon = 2048
const PursuitTimeout = 8 * time.Second
const npcPursuitProjectionDistance = 12

func NPCProjectPosition(
	mesh *basenavigation.Mesh, position game.Vec3, footprintRadius float32,
) (game.Vec3, bool, error) {
	if !zonegeometry.IsFinite(position) ||
		!zonegeometry.IsFinitePositiveScalar(footprintRadius) {
		return game.Vec3{}, false, errors.New("invalid npc projection")
	}
	if mesh == nil {
		return position, false, nil
	}
	planLayer, isLayerFound := mesh.SelectLayer(
		footprintRadius, zonenavigation.HeroHeight,
	)
	if !isLayerFound {
		return position, false, nil
	}
	projection, err := mesh.Project(
		basenavigation.Vec3{X: position.X, Y: position.Y, Z: position.Z},
		basenavigation.ProjectionOptions{
			PlanLayer: planLayer, MaxDistance: npcPursuitProjectionDistance,
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

func NPCDirectMovementDestination(
	mesh *basenavigation.Mesh, source game.Vec3, desired game.Vec3,
	footprintRadius float32,
) (game.Vec3, bool, error) {
	if !zonegeometry.IsFinite(source) || !zonegeometry.IsFinite(desired) ||
		!zonegeometry.IsFinitePositiveScalar(footprintRadius) {
		return game.Vec3{}, false, errors.New("invalid npc direct movement")
	}
	if mesh == nil {
		return source, false, nil
	}
	planLayer, isLayerFound := mesh.SelectLayer(
		footprintRadius, zonenavigation.HeroHeight,
	)
	if !isLayerFound {
		return source, false, nil
	}
	path, err := mesh.CreatePath(
		basenavigation.Vec3{X: source.X, Y: source.Y, Z: source.Z},
		basenavigation.Vec3{X: desired.X, Y: desired.Y, Z: desired.Z},
		basenavigation.PathOptions{
			ProjectionOptions: basenavigation.ProjectionOptions{
				PlanLayer: planLayer, MaxDistance: npcPursuitProjectionDistance,
			},
			MaxVisitedPolygon: maximumVisitedPolygon,
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

func NPCPathClear(
	mesh *basenavigation.Mesh, source game.Vec3, target game.Vec3,
	footprintRadius float32,
) (bool, error) {
	if !zonegeometry.IsFinite(source) || !zonegeometry.IsFinite(target) ||
		!zonegeometry.IsFinitePositiveScalar(footprintRadius) {
		return false, errors.New("invalid npc path visibility")
	}
	if mesh == nil {
		return true, nil
	}
	planLayer, isLayerFound := mesh.SelectLayer(
		footprintRadius, zonenavigation.HeroHeight,
	)
	if !isLayerFound {
		return false, nil
	}
	path, err := mesh.CreatePath(
		basenavigation.Vec3{X: source.X, Y: source.Y, Z: source.Z},
		basenavigation.Vec3{X: target.X, Y: target.Y, Z: target.Z},
		basenavigation.PathOptions{
			ProjectionOptions: basenavigation.ProjectionOptions{
				PlanLayer: planLayer, MaxDistance: npcPursuitProjectionDistance,
			},
			MaxVisitedPolygon: maximumVisitedPolygon,
		},
	)
	if err != nil || len(path.Points) < 2 {
		return false, nil
	}
	pathDistance := float32(0)
	previous := path.Points[0]
	for _, point := range path.Points[1:] {
		deltaX := point.X - previous.X
		deltaY := point.Y - previous.Y
		deltaZ := point.Z - previous.Z
		pathDistance += float32(math.Sqrt(float64(
			deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ,
		)))
		previous = point
	}
	directDistance := target.Sub(source).Length()
	maximumDetour := directDistance*0.2 + footprintRadius*2
	return pathDistance <= directDistance+maximumDetour, nil
}

func AdjustedPursuitRange(abilityRange float32) (float32, error) {
	if !zonegeometry.IsFinitePositiveScalar(abilityRange) {
		return 0, errors.New("invalid campaign ability pursuit range")
	}
	if abilityRange > 10 {
		return abilityRange - 1, nil
	}
	return abilityRange * 0.800000011920929, nil
}

func NPCStopDistance(
	abilityRange float32, actorFootprintRadius float32, targetFootprintRadius float32,
) (float32, error) {
	if actorFootprintRadius < 0 || math.IsNaN(float64(actorFootprintRadius)) ||
		math.IsInf(float64(actorFootprintRadius), 0) ||
		!zonegeometry.IsFinitePositiveScalar(targetFootprintRadius) {
		return 0, errors.New("invalid campaign pursuit footprint")
	}
	adjustedRange, err := AdjustedPursuitRange(abilityRange)
	if err != nil {
		return 0, fmt.Errorf("enemyStopRange: %w", err)
	}
	return actorFootprintRadius + targetFootprintRadius + adjustedRange, nil
}

func AdvancePursuitPath(
	mesh *basenavigation.Mesh, source game.Vec3, target game.Vec3,
	footprintRadius float32, travel float32,
) (game.Vec3, error) {
	if mesh == nil || !zonegeometry.IsFinite(source) ||
		!zonegeometry.IsFinite(target) ||
		!zonegeometry.IsFinitePositiveScalar(footprintRadius) ||
		!zonegeometry.IsFinitePositiveScalar(travel) {
		return game.Vec3{}, errors.New("invalid campaign pursuit path")
	}
	planLayer, isLayerFound := mesh.SelectLayer(
		footprintRadius, zonenavigation.HeroHeight,
	)
	if !isLayerFound {
		planLayer, isLayerFound = mesh.SelectLargestLayer(
			zonenavigation.HeroHeight,
		)
	}
	if !isLayerFound {
		return game.Vec3{}, errors.New("campaign pursuit layer unavailable")
	}
	path, err := mesh.CreatePath(
		basenavigation.Vec3{X: source.X, Y: source.Y, Z: source.Z},
		basenavigation.Vec3{X: target.X, Y: target.Y, Z: target.Z},
		basenavigation.PathOptions{
			ProjectionOptions: basenavigation.ProjectionOptions{
				PlanLayer: planLayer, MaxDistance: npcPursuitProjectionDistance,
			},
			MaxVisitedPolygon: maximumVisitedPolygon,
		},
	)
	if err != nil {
		return game.Vec3{}, fmt.Errorf("campaignPursuitCreate: %w", err)
	}

	position := basenavigation.Vec3{X: source.X, Y: source.Y, Z: source.Z}
	for _, point := range path.Points {
		deltaX := point.X - position.X
		deltaY := point.Y - position.Y
		deltaZ := point.Z - position.Z
		distance := float32(math.Sqrt(float64(
			deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ,
		)))
		if distance == 0 {
			continue
		}
		if travel >= distance {
			position = point
			travel -= distance
			continue
		}
		scale := travel / distance
		position = basenavigation.Vec3{
			X: position.X + deltaX*scale,
			Y: position.Y + deltaY*scale,
			Z: position.Z + deltaZ*scale,
		}
		break
	}
	return game.Vec3{X: position.X, Y: position.Y, Z: position.Z}, nil
}
