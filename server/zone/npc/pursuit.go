package npc

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

const minimumNavigationFootprintRadius = 0.1

type PursuitStep struct {
	Position                 game.Vec3
	IsInRange                bool
	IsNavigationFallback     bool
	NavigationFallbackReason string
}

// PursuitProgress is one transport-neutral NPC movement publication.
type PursuitProgress struct {
	ObjectID       uint32
	Position       game.Vec3
	TargetPosition game.Vec3
	StopDistance   float32
}

type PursuitRequest struct {
	Mesh            *navigation.Mesh
	NounNames       []string
	TargetPosition  game.Vec3
	StopDistance    float32
	Speed           float32
	FootprintRadius float32
	Elapsed         time.Duration
}

func (s *Session) AdvancePursuits(
	req PursuitRequest,
) ([]PursuitProgress, error) {
	if s == nil || len(req.NounNames) == 0 {
		return nil, nil
	}
	nounNames := make(map[string]bool, len(req.NounNames))
	for _, nounName := range req.NounNames {
		if nounName == "" {
			return nil, errors.New("empty npc pursuit noun")
		}
		nounNames[nounName] = true
	}
	objectIDs := s.LiveActorObjectIDs()
	progress := make([]PursuitProgress, 0, len(objectIDs))
	for index, objectID := range objectIDs {
		npc, isFound := s.NPC(objectID)
		if !isFound || !nounNames[npc.Plan.NounName] || npc.IsActionStarted {
			continue
		}
		if zonegeometry.ContainsSphereStrict(
			req.TargetPosition, npc.Plan.Position, req.StopDistance,
		) {
			continue
		}
		step, err := s.AdvancePursuit(
			req.Mesh, objectID, req.TargetPosition, req.StopDistance,
			req.Speed, req.FootprintRadius, req.Elapsed,
		)
		if err != nil {
			return nil, fmt.Errorf("npcPursuit[%d]: %w", index, err)
		}
		progress = append(progress, PursuitProgress{
			ObjectID: objectID, Position: step.Position,
			TargetPosition: req.TargetPosition,
			StopDistance:   req.StopDistance,
		})
	}
	return progress, nil
}

func (s *Session) AdvancePursuit(
	mesh *navigation.Mesh, objectID uint32, targetPosition game.Vec3, stopDistance float32,
	speed float32, footprintRadius float32, elapsed time.Duration,
) (PursuitStep, error) {
	if s == nil || objectID == 0 || !zonegeometry.IsFinite(targetPosition) ||
		!zonegeometry.IsFinitePositiveScalar(stopDistance) ||
		!zonegeometry.IsFinitePositiveScalar(speed) ||
		math.IsNaN(float64(footprintRadius)) ||
		math.IsInf(float64(footprintRadius), 0) || footprintRadius < 0 ||
		elapsed <= 0 {
		return PursuitStep{}, errors.New("invalid npc pursuit")
	}
	if mesh != nil {
		footprintRadius = max(footprintRadius, minimumNavigationFootprintRadius)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || npc.HitPoint <= 0 {
		return PursuitStep{}, errors.New("npc pursuit unavailable")
	}
	source := npc.Plan.Position
	deltaX := targetPosition.X - source.X
	deltaY := targetPosition.Y - source.Y
	deltaZ := targetPosition.Z - source.Z
	distance := zonegeometry.Distance(source, targetPosition)
	if distance < stopDistance {
		return PursuitStep{Position: source, IsInRange: true}, nil
	}
	travel := speed * float32(elapsed.Seconds())
	// A late callback may cover several client frames. Stop at attack range
	// instead of spending that entire interval moving into the target's center.
	travel = min(travel, max(float32(0.0001), distance-stopDistance+0.0001))
	if mesh != nil {
		destination, pathErr := zoneaction.AdvancePursuitPath(
			mesh, source, targetPosition, footprintRadius, travel,
		)
		if pathErr == nil && destination != source {
			npc.Facing = directionTo(source, destination)
			npc.Plan.Position = destination
			s.npcs[objectID] = npc
			return PursuitStep{
				Position:  destination,
				IsInRange: zonegeometry.Distance(destination, targetPosition) < stopDistance,
			}, nil
		}
		planarDistance := float32(math.Hypot(float64(deltaX), float64(deltaY)))
		if planarDistance < stopDistance &&
			float32(math.Abs(float64(deltaZ))) <= zonenavigation.ProjectionDistance {
			return PursuitStep{Position: source, IsInRange: true}, nil
		}
		navigationFallbackReason := "path did not advance"
		if pathErr != nil {
			navigationFallbackReason = pathErr.Error()
		}
		// The client continues the published locomotion directly toward its
		// goal when the authored navigation mesh cannot project either end of
		// the route. Advance the authoritative pose along that same fallback
		// path so range checks do not keep using the NPC's stale spawn point.
		remaining := distance - stopDistance
		if travel >= remaining {
			travel = min(distance, remaining+0.0001)
		}
		scale := travel / distance
		npc.Plan.Position = game.Vec3{
			X: source.X + deltaX*scale,
			Y: source.Y + deltaY*scale,
			Z: source.Z + deltaZ*scale,
		}
		npc.Facing = directionTo(source, npc.Plan.Position)
		s.npcs[objectID] = npc
		return PursuitStep{
			Position: npc.Plan.Position,
			IsInRange: zonegeometry.Distance(
				npc.Plan.Position, targetPosition,
			) < stopDistance,
			IsNavigationFallback:     true,
			NavigationFallbackReason: navigationFallbackReason,
		}, nil
	}
	remaining := distance - stopDistance
	if travel >= remaining {
		travel = min(distance, remaining+0.0001)
	}
	scale := travel / distance
	npc.Plan.Position = game.Vec3{
		X: source.X + deltaX*scale,
		Y: source.Y + deltaY*scale,
		Z: source.Z + deltaZ*scale,
	}
	npc.Facing = directionTo(source, npc.Plan.Position)
	s.npcs[objectID] = npc
	return PursuitStep{
		Position:  npc.Plan.Position,
		IsInRange: zonegeometry.Distance(npc.Plan.Position, targetPosition) < stopDistance,
	}, nil
}
