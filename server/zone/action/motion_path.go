package action

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/sim"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

// SetNavigation supplies the same immutable mesh and footprint used by command
// admission. Reconciliation rebuilds the remaining route from the accepted pose.
func (e *Motion) SetNavigation(mesh *navigation.Mesh, footprintRadius float32) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.navigation = mesh
	e.footprintRadius = footprintRadius
}

func (e *Motion) setGoal(
	movement *sim.LinearMovement, at time.Duration,
	position sim.Position, goal sim.Position, speed float32,
	projectionRange float32,
) (sim.Position, error) {
	if e.navigation == nil {
		result, err := movement.SetGoal(at, goal, speed)
		if err != nil {
			return sim.Position{}, fmt.Errorf("linearGoal: %w", err)
		}
		return result, nil
	}
	planLayer, isLayerFound := e.navigation.SelectLayer(
		e.footprintRadius, zonenavigation.HeroHeight,
	)
	if !isLayerFound {
		return sim.Position{}, errors.New("movement layer unavailable")
	}
	path, err := e.navigation.CreatePath(
		navigation.Vec3(position), navigation.Vec3(goal),
		navigation.PathOptions{
			ProjectionOptions: navigation.ProjectionOptions{
				PlanLayer: planLayer, MaxDistance: projectionRange,
			},
			MaxVisitedPolygon: maximumVisitedPolygon,
		},
	)
	if err != nil {
		return sim.Position{}, fmt.Errorf("routeCreate: %w", err)
	}
	points := make([]sim.Position, 0, len(path.Points))
	for _, point := range path.Points {
		points = append(points, sim.Position(point))
	}
	result, err := movement.SetPath(at, points, speed)
	if err != nil {
		return sim.Position{}, fmt.Errorf("routeSet: %w", err)
	}
	return result, nil
}

// reconcilePosition bounds a correction by reachable route length, rather
// than allowing a short straight-line correction across a wall or floor.
func (e *Motion) reconcilePosition(
	movement *sim.LinearMovement, at time.Duration,
	reported sim.Position, correctionRange float32,
) (sim.Position, bool, error) {
	if e.navigation == nil {
		position, isAccepted, err := movement.Reconcile(at, reported, correctionRange)
		if err != nil {
			return sim.Position{}, false, fmt.Errorf("linearReconcile: %w", err)
		}
		return position, isAccepted, nil
	}
	probe := movement.Clone()
	position, isAccepted, err := probe.Reconcile(at, reported, correctionRange)
	if err != nil {
		return sim.Position{}, false, fmt.Errorf("poseValidate: %w", err)
	}
	if !isAccepted {
		return position, false, nil
	}
	position = movement.Snapshot().Position
	planLayer, isLayerFound := e.navigation.SelectLayer(e.footprintRadius, zonenavigation.HeroHeight)
	if !isLayerFound {
		return position, false, nil
	}
	path, err := e.navigation.CreatePath(
		navigation.Vec3(position), navigation.Vec3(reported),
		navigation.PathOptions{
			ProjectionOptions: navigation.ProjectionOptions{
				PlanLayer: planLayer, MaxDistance: zonenavigation.ProjectionDistance,
			},
			MaxVisitedPolygon: maximumVisitedPolygon,
		},
	)
	if err != nil {
		// An unreachable observation is rejected; the running authoritative
		// route remains valid and continues instead of failing the command.
		return position, false, nil
	}
	distance := path.Start.Distance + path.Goal.Distance
	for index := 1; index < len(path.Points); index++ {
		previous, current := path.Points[index-1], path.Points[index]
		deltaX, deltaY, deltaZ := current.X-previous.X, current.Y-previous.Y, current.Z-previous.Z
		distance += float32(math.Sqrt(float64(deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ)))
	}
	if distance > correctionRange {
		return position, false, nil
	}
	position, isAccepted, err = movement.Reconcile(at, sim.Position(path.Goal.Position), correctionRange)
	if err != nil {
		return sim.Position{}, false, fmt.Errorf("routeReconcile: %w", err)
	}
	return position, isAccepted, nil
}
