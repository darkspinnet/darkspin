package navigation

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	basenavigation "github.com/darkspinnet/darkspin/server/navigation"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type DeploymentRequest struct {
	Position        game.Vec3
	AnchorPosition  game.Vec3
	IsAnchorPresent bool
	FootprintRadius float32
	Mode            uint8
}

// DeploymentPosition admits the incoming actor's actual navigation layer.
// A blocked current pose can roll back only to this actor's recent validated
// placement, never to an arbitrary free face on the other side of a collider.
func DeploymentPosition(mesh *basenavigation.Mesh, req DeploymentRequest) (game.Vec3, error) {
	if !zonegeometry.IsFinite(req.Position) || req.FootprintRadius < 0 {
		return game.Vec3{}, errors.New("deployment placement invalid")
	}
	if mesh == nil {
		return req.Position, nil
	}
	planLayer, isLayerFound := mesh.SelectFootprintLayer(req.FootprintRadius, req.Mode)
	if !isLayerFound {
		return game.Vec3{}, errors.New("deployment layer unavailable")
	}
	position, err := projectDeploymentPosition(mesh, req.Position, planLayer)
	if err == nil {
		return position, nil
	}
	if !req.IsAnchorPresent || !zonegeometry.IsFinite(req.AnchorPosition) ||
		req.AnchorPosition.Sub(req.Position).Length() > ProjectionDistance {
		return game.Vec3{}, fmt.Errorf("deploymentCurrent: %w", err)
	}
	position, anchorErr := projectDeploymentPosition(mesh, req.AnchorPosition, planLayer)
	if anchorErr != nil {
		return game.Vec3{}, fmt.Errorf("deploymentAnchor: %w", errors.Join(err, anchorErr))
	}
	return position, nil
}

func projectDeploymentPosition(mesh *basenavigation.Mesh, position game.Vec3, planLayer uint8) (game.Vec3, error) {
	projection, err := mesh.Project(basenavigation.Vec3(position), basenavigation.ProjectionOptions{
		PlanLayer: planLayer, MaxDistance: ProjectionDistance,
	})
	if err != nil {
		return game.Vec3{}, fmt.Errorf("deploymentProject: %w", err)
	}
	if !mesh.IsSegmentClear(basenavigation.Vec3(position), projection.Position, planLayer) {
		return game.Vec3{}, errors.New("deployment projection crosses obstacle")
	}
	return game.Vec3(projection.Position), nil
}
