package action

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	basenavigation "github.com/darkspinnet/darkspin/server/navigation"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

const maximumVisitedPolygon = 2048
const PursuitTimeout = 8 * time.Second
const pursuitProjectionDistance = 12

// PlayerPursuitDestination retains the NPC center for ordinary pursuit. When
// that center is inside a collision blocker, approach a reachable contact in
// attack range instead of requiring the hero to stand inside the same blocker.
func PlayerPursuitDestination(mesh *basenavigation.Mesh, source game.Vec3, target game.Vec3, radius float32, mode uint8, stopDistance float32) (game.Vec3, error) {
	if mesh == nil {
		return target, nil
	}
	planLayer, isFound := mesh.SelectFootprintLayer(radius, mode)
	if !isFound {
		return target, nil
	}
	options := basenavigation.ProjectionOptions{PlanLayer: planLayer, MaxDistance: pursuitProjectionDistance}
	projection, err := mesh.Project(basenavigation.Vec3(target), options)
	if !errors.Is(err, basenavigation.ErrProjectionObstructed) {
		// The ordinary path remains responsible for other projection errors.
		// A successful projection does not change its existing goal semantics.
		return target, nil
	}
	projection, err = mesh.Project(basenavigation.Vec3(source), options)
	if err != nil {
		return game.Vec3{}, fmt.Errorf("pursuitContactStart: %w", err)
	}
	options.MaxDistance = stopDistance
	options.ComponentID = projection.ComponentID
	options.IsComponentConstrained = true
	contact, err := mesh.ProjectInteractionTarget(projection, basenavigation.Vec3(target), options)
	if err != nil {
		return game.Vec3{}, fmt.Errorf("pursuitContactGoal: %w", err)
	}
	return game.Vec3(contact.Position), nil
}

func NPCProjectPosition(
	mesh *basenavigation.Mesh, position game.Vec3, footprintRadius float32, actors ...basenavigation.ActorNavigation,
) (game.Vec3, bool, error) {
	if !zonegeometry.IsFinite(position) ||
		(!zonegeometry.IsFiniteScalar(footprintRadius) || footprintRadius < 0) {
		return game.Vec3{}, false, errors.New("invalid npc projection")
	}
	if mesh == nil {
		return position, false, nil
	}
	planLayer, isLayerFound := mesh.ActorLayer(footprintRadius, actors...)
	if !isLayerFound {
		return position, false, nil
	}
	projection, err := mesh.Project(
		basenavigation.Vec3{X: position.X, Y: position.Y, Z: position.Z},
		basenavigation.ProjectionOptions{
			PlanLayer: planLayer, MaxDistance: pursuitProjectionDistance,
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
	footprintRadius float32, actors ...basenavigation.ActorNavigation,
) (game.Vec3, bool, error) {
	if !zonegeometry.IsFinite(source) || !zonegeometry.IsFinite(desired) ||
		(!zonegeometry.IsFiniteScalar(footprintRadius) || footprintRadius < 0) {
		return game.Vec3{}, false, errors.New("invalid npc direct movement")
	}
	if mesh == nil {
		return source, false, nil
	}
	planLayer, isLayerFound := mesh.ActorLayer(footprintRadius, actors...)
	if !isLayerFound {
		return source, false, nil
	}
	path, err := mesh.CreatePath(
		basenavigation.Vec3{X: source.X, Y: source.Y, Z: source.Z},
		basenavigation.Vec3{X: desired.X, Y: desired.Y, Z: desired.Z},
		basenavigation.PathOptions{
			ProjectionOptions: basenavigation.ProjectionOptions{
				PlanLayer: planLayer, MaxDistance: pursuitProjectionDistance,
			},
			MaxVisitedPolygon: maximumVisitedPolygon,
		},
	)
	if err != nil || len(path.Points) < 2 {
		return source, false, nil
	}
	if len(path.Points) == 2 {
		return game.Vec3{
			X: path.Goal.Position.X,
			Y: path.Goal.Position.Y,
			Z: path.Goal.Position.Z,
		}, true, nil
	}
	// The navigation funnel retains portal crossings along a straight route.
	// Accept those points while rejecting bends and off-ray projections.
	deltaX, deltaY := desired.X-source.X, desired.Y-source.Y
	lengthSquared := deltaX*deltaX + deltaY*deltaY
	if lengthSquared <= 0.000000000001 {
		return source, false, nil
	}
	const planarEpsilon = 0.001
	tolerance := float64(planarEpsilon) * math.Sqrt(float64(lengthSquared))
	previousAlong := float32(0)
	for _, point := range path.Points {
		pointX, pointY := point.X-source.X, point.Y-source.Y
		along := pointX*deltaX + pointY*deltaY
		cross := pointX*deltaY - pointY*deltaX
		if float64(along-previousAlong) < -tolerance ||
			float64(along-lengthSquared) > tolerance ||
			math.Abs(float64(cross)) > tolerance {
			return source, false, nil
		}
		previousAlong = along
	}
	goal := path.Goal.Position
	return game.Vec3{X: goal.X, Y: goal.Y, Z: goal.Z}, true, nil
}

func NPCPathClear(
	mesh *basenavigation.Mesh, source game.Vec3, target game.Vec3,
	footprintRadius float32, actors ...basenavigation.ActorNavigation,
) (bool, error) {
	if !zonegeometry.IsFinite(source) || !zonegeometry.IsFinite(target) ||
		(!zonegeometry.IsFiniteScalar(footprintRadius) || footprintRadius < 0) {
		return false, errors.New("invalid npc path visibility")
	}
	if mesh == nil {
		return true, nil
	}
	planLayer, isLayerFound := mesh.ActorLayer(footprintRadius, actors...)
	if !isLayerFound {
		return false, nil
	}
	path, err := mesh.CreatePath(
		basenavigation.Vec3{X: source.X, Y: source.Y, Z: source.Z},
		basenavigation.Vec3{X: target.X, Y: target.Y, Z: target.Z},
		basenavigation.PathOptions{
			ProjectionOptions: basenavigation.ProjectionOptions{
				PlanLayer: planLayer, MaxDistance: pursuitProjectionDistance,
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
	footprintRadius float32, travel float32, actors ...basenavigation.ActorNavigation,
) (game.Vec3, error) {
	step, err := AdvancePursuitPathStep(mesh, source, target, footprintRadius, travel, actors...)
	if err != nil {
		return game.Vec3{}, fmt.Errorf("pursuitPathStep: %w", err)
	}
	return step.Position, nil
}

type PursuitPathStep struct {
	Position game.Vec3
	Waypoint game.Vec3
}

// Return the same next corridor point used to advance the server actor, so
// remote locomotion can follow that segment rather than steering through walls.
func AdvancePursuitPathStep(
	mesh *basenavigation.Mesh, source game.Vec3, target game.Vec3,
	footprintRadius float32, travel float32, actors ...basenavigation.ActorNavigation,
) (PursuitPathStep, error) {
	if mesh == nil || !zonegeometry.IsFinite(source) ||
		!zonegeometry.IsFinite(target) ||
		(!zonegeometry.IsFiniteScalar(footprintRadius) || footprintRadius < 0) ||
		!zonegeometry.IsFinitePositiveScalar(travel) {
		return PursuitPathStep{}, errors.New("invalid campaign pursuit path")
	}
	planLayer, isLayerFound := mesh.ActorLayer(footprintRadius, actors...)
	if !isLayerFound {
		return PursuitPathStep{}, errors.New("campaign pursuit layer unavailable")
	}
	path, err := mesh.CreatePath(
		basenavigation.Vec3{X: source.X, Y: source.Y, Z: source.Z},
		basenavigation.Vec3{X: target.X, Y: target.Y, Z: target.Z},
		basenavigation.PathOptions{
			ProjectionOptions: basenavigation.ProjectionOptions{
				PlanLayer: planLayer, MaxDistance: pursuitProjectionDistance,
			},
			MaxVisitedPolygon: maximumVisitedPolygon,
		},
	)
	if err != nil {
		return PursuitPathStep{}, fmt.Errorf("campaignPursuitCreate: %w", err)
	}

	position := basenavigation.Vec3{X: source.X, Y: source.Y, Z: source.Z}
	waypoint := target
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
		waypoint = game.Vec3{X: point.X, Y: point.Y, Z: point.Z}
		position = basenavigation.Vec3{
			X: position.X + deltaX*scale,
			Y: position.Y + deltaY*scale,
			Z: position.Z + deltaZ*scale,
		}
		break
	}
	return PursuitPathStep{
		Position: game.Vec3{X: position.X, Y: position.Y, Z: position.Z}, Waypoint: waypoint,
	}, nil
}
