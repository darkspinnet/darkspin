// Package navigation owns campaign traversal validation. It deliberately has
// no RakNet dependency so movement, abilities, interactions, drops, and
// teleporters can share one navmesh policy.
package navigation

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	basenavigation "github.com/darkspinnet/darkspin/server/navigation"
)

const ProjectionDistance = float32(3)
const HeroHeight = float32(1.75)
const interactionProjectionDistance = float32(6)

func ValidateMovement(
	mesh *basenavigation.Mesh, start game.Vec3, goal game.Vec3, footprintRadius float32,
) error {
	if mesh == nil {
		return nil
	}
	if footprintRadius <= 0 {
		return errors.New("navigation footprint invalid")
	}
	planLayer, isLayerFound := mesh.SelectLayer(footprintRadius, HeroHeight)
	if !isLayerFound {
		return errors.New("navigation layer unavailable")
	}
	options := basenavigation.ProjectionOptions{
		PlanLayer: planLayer, MaxDistance: ProjectionDistance,
	}
	startProjection, err := mesh.Project(basenavigation.Vec3{
		X: start.X, Y: start.Y, Z: start.Z,
	}, options)
	if err != nil {
		return fmt.Errorf("navigationStart: %w", err)
	}
	options.ComponentID = startProjection.ComponentID
	options.IsComponentConstrained = true
	goalProjection, err := mesh.Project(basenavigation.Vec3{
		X: goal.X, Y: goal.Y, Z: goal.Z,
	}, options)
	if err != nil {
		return fmt.Errorf("navigationGoal: %w", err)
	}
	if !mesh.IsReachable(startProjection, goalProjection, planLayer) {
		return errors.New("navigation goal unreachable")
	}
	return nil
}

func ValidateTeleport(
	mesh *basenavigation.Mesh, destination game.Vec3, footprintRadius float32,
) error {
	if mesh == nil {
		return nil
	}
	if footprintRadius <= 0 {
		return errors.New("navigation teleport footprint invalid")
	}
	planLayer, isLayerFound := mesh.SelectLayer(footprintRadius, HeroHeight)
	if !isLayerFound {
		return errors.New("navigation teleport layer unavailable")
	}
	_, err := mesh.Project(
		basenavigation.Vec3{
			X: destination.X, Y: destination.Y, Z: destination.Z,
		},
		basenavigation.ProjectionOptions{
			PlanLayer: planLayer, MaxDistance: ProjectionDistance,
		},
	)
	if err != nil {
		return fmt.Errorf("navigationTeleport: %w", err)
	}
	return nil
}

func ValidateInteraction(
	mesh *basenavigation.Mesh, start game.Vec3, target game.Vec3, footprintRadius float32,
) error {
	if mesh == nil {
		return nil
	}
	if footprintRadius <= 0 {
		return errors.New("navigation interaction footprint invalid")
	}
	planLayer, isLayerFound := mesh.SelectLayer(footprintRadius, HeroHeight)
	if !isLayerFound {
		return errors.New("navigation interaction layer unavailable")
	}
	startProjection, err := mesh.Project(basenavigation.Vec3{
		X: start.X, Y: start.Y, Z: start.Z,
	}, basenavigation.ProjectionOptions{
		PlanLayer: planLayer, MaxDistance: ProjectionDistance,
	})
	if err != nil {
		return fmt.Errorf("navigationInteractionStart: %w", err)
	}
	targetProjection, err := mesh.Project(basenavigation.Vec3{
		X: target.X, Y: target.Y, Z: target.Z,
	}, basenavigation.ProjectionOptions{
		PlanLayer: planLayer, MaxDistance: interactionProjectionDistance,
		ComponentID: startProjection.ComponentID, IsComponentConstrained: true,
	})
	if err != nil {
		return fmt.Errorf("navigationInteractionTarget: %w", err)
	}
	if !mesh.IsReachable(startProjection, targetProjection, planLayer) {
		return errors.New("navigation interaction target unreachable")
	}
	return nil
}
