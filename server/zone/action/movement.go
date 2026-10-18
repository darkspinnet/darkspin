// Package action owns player action admission and execution policy shared by
// campaign levels. It has no RakNet dependency; the gameplay transport adapts
// wire commands into these commands.
package action

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	basenavigation "github.com/darkspinnet/darkspin/server/navigation"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

type MovementCommand struct {
	NavigationActor  basenavigation.ActorNavigation
	ObjectID         uint32
	DeployedObjectID uint32
	Position         game.Vec3
	Goal             game.Vec3
	FootprintRadius  float32
	Navigation       *basenavigation.Mesh
}

type MovementAdmission struct {
	Goal            game.Vec3
	IsObjectOwned   bool
	IsPositionValid bool
	IsGoalValid     bool
	IsAccepted      bool
}

func AdmitMovement(command MovementCommand) (MovementAdmission, error) {
	admission := MovementAdmission{
		Goal:            command.Goal,
		IsObjectOwned:   command.ObjectID == command.DeployedObjectID,
		IsPositionValid: zonegeometry.IsFinite(command.Position),
		IsGoalValid:     zonegeometry.IsFinite(command.Goal),
	}
	if !admission.IsObjectOwned || !admission.IsPositionValid || !admission.IsGoalValid {
		return admission, nil
	}
	goal, err := zonenavigation.ResolveMovementGoal(
		command.Navigation, command.Position, command.Goal,
		command.FootprintRadius, command.NavigationActor,
	)
	if err != nil {
		return admission, fmt.Errorf("movementNavigation: %w", err)
	}
	admission.Goal = goal
	admission.IsAccepted = true
	return admission, nil
}
