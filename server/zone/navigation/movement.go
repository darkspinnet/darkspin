package navigation

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	basenavigation "github.com/darkspinnet/darkspin/server/navigation"
)

// ResolveMovementGoal keeps a blocked click at a walkable contact point instead
// of rejecting the actor's entire movement. This uses the server collision-box
// overlay; it does not reproduce native topology splitting around obstacles.
func ResolveMovementGoal(
	mesh *basenavigation.Mesh, start game.Vec3, goal game.Vec3,
	footprintRadius float32, actor basenavigation.ActorNavigation,
) (game.Vec3, error) {
	if mesh == nil {
		return goal, nil
	}
	if footprintRadius < 0 {
		return game.Vec3{}, errors.New("navigation footprint invalid")
	}
	planLayer, isLayerFound := mesh.ActorLayer(footprintRadius, actor)
	if !isLayerFound {
		return game.Vec3{}, errors.New("navigation layer unavailable")
	}
	options := basenavigation.ProjectionOptions{
		PlanLayer: planLayer, MaxDistance: ProjectionDistance,
	}
	startProjection, err := mesh.Project(basenavigation.Vec3(start), options)
	if err != nil {
		return game.Vec3{}, fmt.Errorf("movementStart: %w", err)
	}
	options.ComponentID = startProjection.ComponentID
	options.IsComponentConstrained = true
	// Move clicks can carry the actor's current Z rather than the clicked
	// stair/ramp height. Keep the three-unit XY tolerance and recover Z from
	// the same connected walkable surface; placement/teleport checks stay 3D.
	options.IsPlanar = true
	goalProjection, err := mesh.Project(basenavigation.Vec3(goal), options)
	if errors.Is(err, basenavigation.ErrProjectionObstructed) {
		goalProjection, err = mesh.ProjectMovementTarget(
			startProjection, basenavigation.Vec3(goal), options,
		)
	}
	if err != nil {
		return game.Vec3{}, fmt.Errorf("movementGoal: %w", err)
	}
	if !mesh.IsReachable(startProjection, goalProjection, planLayer) {
		return game.Vec3{}, errors.New("navigation goal unreachable")
	}
	return game.Vec3(goalProjection.Position), nil
}
