package companion

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
)

// ConfigureNavigation shares the selected zone's collision lifetime. Walking
// actors cache their footprint layer; explicit flying and forced paths opt out.
func (e *Session) ConfigureNavigation(mesh *navigation.Mesh) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.navigationMesh = mesh
	for objectID, actor := range e.actors {
		e.actors[objectID] = e.configureActorNavigation(actor)
	}
}

func (e *Session) configureActorNavigation(actor Actor) Actor {
	actor.navigationMesh = e.navigationMesh
	if e.navigationMesh == nil {
		return actor
	}
	if actor.Navigation.IsPresent {
		layer, isFound := e.navigationMesh.LayerInfo(actor.Navigation.PlanLayer)
		if isFound && layer.PlanLayer == actor.Navigation.PlanLayer {
			return actor
		}
		actor.Navigation = navigation.ActorNavigation{}
	}
	radius := max(float32(.1), actor.ActorFootprintRadius())
	layer, isFound := e.navigationMesh.SelectFootprintLayer(radius)
	if !isFound {
		// Stationary and explicitly flying actors need no walking layer.
		// Keep their registration unchanged; walkingPaths rejects unavailable
		// ground navigation instead of inventing a straight movement fallback.
		return actor
	}
	actor.Navigation = navigation.ActorNavigation{Radius: radius, PlanLayer: layer, IsPresent: true}
	return actor
}

func (e *Session) walkingPaths(actor Actor, destination game.Vec3) ([]game.Vec3, game.Vec3, error) {
	if e.navigationMesh == nil {
		return nil, destination, nil
	}
	if !actor.Navigation.IsPresent {
		return nil, game.Vec3{}, errors.New("companion navigation unavailable")
	}
	path, err := e.navigationMesh.CreatePath(navigation.Vec3(actor.Position), navigation.Vec3(destination), navigation.PathOptions{
		ProjectionOptions: navigation.ProjectionOptions{PlanLayer: actor.Navigation.PlanLayer, MaxDistance: 3},
		MaxVisitedPolygon: 2048,
	})
	if err != nil {
		return nil, game.Vec3{}, fmt.Errorf("companionPath: %w", err)
	}
	points := make([]game.Vec3, 0, len(path.Points)+1)
	points = append(points, actor.Position)
	for _, point := range path.Points {
		position := game.Vec3(point)
		if position != points[len(points)-1] {
			points = append(points, position)
		}
	}
	if len(points) > 1 && !e.navigationMesh.IsSegmentClear(navigation.Vec3(points[0]), navigation.Vec3(points[1]), actor.Navigation.PlanLayer) {
		return nil, game.Vec3{}, errors.New("companion path start obstructed")
	}
	return points, game.Vec3(path.Goal.Position), nil
}

func walkingDistance(points []game.Vec3) float32 {
	distance := float32(0)
	for index := 1; index < len(points); index++ {
		distance += points[index].Sub(points[index-1]).Length()
	}
	return distance
}

func walkingFraction(start, deadline, at time.Time) float32 {
	if !at.After(start) {
		return 0
	}
	if !at.Before(deadline) {
		return 1
	}
	return float32(float64(at.Sub(start)) / float64(deadline.Sub(start)))
}

// Only already admitted terrain segments are sampled here. Each sample also
// checks the current shared collision overlay before accepting travel.
type walkingSample struct {
	position  game.Vec3
	nextIndex int
	isBlocked bool
}

func (e Actor) walkingPosition(points []game.Vec3, fraction float32) walkingSample {
	remaining := walkingDistance(points) * fraction
	position := points[0]
	for index := 1; index < len(points); index++ {
		delta := points[index].Sub(position)
		length := delta.Length()
		next := points[index]
		if length > remaining && length > 0 {
			next = position.Add(delta.Scale(remaining / length))
		}
		if e.navigationMesh != nil && !e.navigationMesh.IsSegmentClear(navigation.Vec3(position), navigation.Vec3(next), e.Navigation.PlanLayer) {
			return walkingSample{position: position, nextIndex: index, isBlocked: true}
		}
		if length >= remaining {
			return walkingSample{position: next, nextIndex: index}
		}
		remaining -= length
		position = next
	}
	return walkingSample{position: position, nextIndex: len(points)}
}

func remainingWalkingPaths(points []game.Vec3, position game.Vec3, index int) []game.Vec3 {
	remainingPoints := make([]game.Vec3, 0, len(points)-index+1)
	remainingPoints = append(remainingPoints, position)
	for _, point := range points[index:] {
		if point != remainingPoints[len(remainingPoints)-1] {
			remainingPoints = append(remainingPoints, point)
		}
	}
	return remainingPoints
}

func (e *Session) refreshWalkingPathsLocked(actor Actor, at time.Time) Actor {
	if len(actor.followPaths) != 0 && actor.IsFollowing && at.After(actor.followStartedAt) {
		sample := actor.walkingPosition(actor.followPaths, walkingFraction(actor.followStartedAt, actor.followDeadline, at))
		actor.Position = sample.position
		actor.followPaths = remainingWalkingPaths(actor.followPaths, sample.position, sample.nextIndex)
		actor.followPlan.Position = sample.position
		actor.followStartedAt = at
		if sample.isBlocked {
			actor.followPaths = nil
			actor.followPlan.Destination = sample.position
			actor.followDeadline = at
			e.motionRevision++
		}
		actor.followPlan.TravelDuration = max(time.Duration(0), actor.followDeadline.Sub(at))
		actor.followPlan.Deadline = actor.followDeadline
	}
	if len(actor.pursuitPaths) != 0 && actor.PursuitObjectID != 0 && at.After(actor.pursuitStartedAt) {
		sample := actor.walkingPosition(actor.pursuitPaths, walkingFraction(actor.pursuitStartedAt, actor.pursuitDeadline, at))
		actor.Position = sample.position
		actor.pursuitPaths = remainingWalkingPaths(actor.pursuitPaths, sample.position, sample.nextIndex)
		actor.pursuitSource = sample.position
		actor.pursuitStartedAt = at
		if sample.isBlocked {
			actor.pursuitPaths = nil
			actor.PursuitPosition = sample.position
			actor.pursuitDeadline = at
			e.motionRevision++
		}
	}
	return actor
}
