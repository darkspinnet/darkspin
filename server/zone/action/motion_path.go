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
	e.SetActorNavigation(mesh, footprintRadius, 0, 0)
}

func (e *Motion) SetActorNavigation(mesh *navigation.Mesh, footprintRadius float32, objectID uint32, mode uint8) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	isSameActor := objectID != 0 && objectID == e.navigationObjectID
	if e.navigation == mesh && e.navigationActor.IsPresent &&
		(isSameActor || (objectID == 0 && e.footprintRadius == footprintRadius)) {
		return
	}
	e.navigationObjectID = objectID
	e.navigation = mesh
	e.isNavigationAnchorPresent = false
	e.footprintRadius = footprintRadius
	e.navigationActor = navigation.ActorNavigation{Radius: footprintRadius, Mode: mode}
	if mesh != nil {
		planLayer, isFound := mesh.SelectFootprintLayer(footprintRadius, mode)
		if isFound {
			e.navigationActor.PlanLayer = planLayer
			e.navigationActor.IsPresent = true
		}
	}
	e.rememberNavigationPositionLocked(e.position)
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
	planLayer, isLayerFound := e.navigation.ActorLayer(e.footprintRadius, e.navigationActor)
	if !isLayerFound {
		return sim.Position{}, errors.New("movement layer unavailable")
	}
	path, err := e.navigation.CreatePath(
		navigation.Vec3(position), navigation.Vec3(goal),
		navigation.PathOptions{
			ProjectionOptions: navigation.ProjectionOptions{
				PlanLayer: planLayer, MaxDistance: projectionRange,
			},
			MaxVisitedPolygon:  maximumVisitedPolygon,
			IsTraversalAllowed: true,
		},
	)
	if err != nil {
		return sim.Position{}, fmt.Errorf("routeCreate: %w", err)
	}
	pointCount := len(path.Points)
	for _, traversal := range path.Traversals {
		if traversal.PointIndex < 0 || traversal.PointIndex >= len(path.Points) {
			return sim.Position{}, errors.New("movement traversal entry unavailable")
		}
		pointCount = min(pointCount, traversal.PointIndex+1)
	}
	// Motion walks only as far as the first authored trigger entry. The normal
	// encounter observer performs its transition; the destination and departure
	// route must never become a LinearMovement segment or goal.
	points := make([]sim.Position, 0, pointCount)
	for _, point := range path.Points[:pointCount] {
		points = append(points, sim.Position(point))
	}
	result, err := movement.SetPath(at, points, speed)
	if err != nil {
		return sim.Position{}, fmt.Errorf("routeSet: %w", err)
	}
	// CreatePath already validated this terrain point and collision clearance;
	// do not repeat a projection for every movement/pose observation.
	e.navigationAnchor = sim.Position(path.Start.Position)
	e.isNavigationAnchorPresent = true
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
	planLayer, isLayerFound := e.navigation.ActorLayer(e.footprintRadius, e.navigationActor)
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
	if len(path.Traversals) != 0 {
		// An authored jump cannot justify an observed-pose correction, even
		// when its walking legs fit inside the ordinary drift allowance.
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

func (e *Motion) NavigationSettings() navigation.ActorNavigation {
	if e == nil {
		return navigation.ActorNavigation{}
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.navigationActor
}
