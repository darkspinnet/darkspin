package action

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/sim"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

// Call with the motion lock held. Retain only an actual free terrain point,
// not a predicted observation or a future movement goal.
func (e *Motion) rememberNavigationPositionLocked(position sim.Position) {
	if e.navigation == nil || !e.navigationActor.IsPresent {
		return
	}
	if e.isNavigationAnchorPresent && position == e.navigationAnchor {
		return
	}
	projection, err := e.navigation.Project(navigation.Vec3(position), navigation.ProjectionOptions{
		PlanLayer: e.navigationActor.PlanLayer, MaxDistance: zonenavigation.ProjectionDistance,
	})
	if err != nil || !e.navigation.IsSegmentClear(navigation.Vec3(position), projection.Position, e.navigationActor.PlanLayer) {
		return
	}
	e.navigationAnchor = sim.Position(projection.Position)
	e.isNavigationAnchorPresent = true
}

func (e *Motion) DeploymentPosition(at time.Time, footprintRadius float32, mode uint8) (sim.Position, error) {
	if e == nil {
		return sim.Position{}, errors.New("nil deployment motion")
	}
	e.mu.RLock()
	if e.movement == nil || at.Before(e.startedAt) {
		e.mu.RUnlock()
		return sim.Position{}, errors.New("deployment motion unavailable")
	}
	movement := e.movement.Clone()
	elapsed := max(at.Sub(e.startedAt), movement.Snapshot().At)
	mesh := e.navigation
	anchor := e.navigationAnchor
	isAnchorPresent := e.isNavigationAnchorPresent
	e.mu.RUnlock()
	position, err := movement.Position(elapsed)
	if err != nil {
		return sim.Position{}, fmt.Errorf("deploymentSample: %w", err)
	}
	destination, err := zonenavigation.DeploymentPosition(mesh, zonenavigation.DeploymentRequest{
		Position: game.Vec3(position), AnchorPosition: game.Vec3(anchor), IsAnchorPresent: isAnchorPresent,
		FootprintRadius: footprintRadius, Mode: mode,
	})
	if err != nil {
		return sim.Position{}, fmt.Errorf("deploymentPlacement: %w", err)
	}
	return sim.Position(destination), nil
}
