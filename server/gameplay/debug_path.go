//go:build scenario

package gameplay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/scenario"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

type scenarioWorldPathProvider interface {
	ScenarioWorldPath(context.Context, scenario.WorldPathRequest) (scenario.WorldPath, error)
}

type worldPathContext struct {
	mesh            *navigation.Mesh
	objectID        uint32
	planLayer       uint8
	footprintRadius float32
}

func (e Lifecycle) ScenarioWorldPath(ctx context.Context, req scenario.WorldPathRequest) (scenario.WorldPath, error) {
	provider, isSupported := e.syncSnapshot.(scenarioWorldPathProvider)
	if !isSupported {
		return scenario.WorldPath{}, errors.New("world navigation provider is unavailable")
	}
	result, err := provider.ScenarioWorldPath(ctx, req)
	if err != nil {
		return result, fmt.Errorf("pathProvider: %w", err)
	}
	return result, nil
}

func (e *gameplaySessionRegistry) ScenarioWorldPath(ctx context.Context, req scenario.WorldPathRequest) (scenario.WorldPath, error) {
	result := scenario.WorldPath{CapturedAt: time.Now().UTC(), GameID: req.GameID, PeerGeneration: req.PeerGeneration, TransportGeneration: req.TransportGeneration, ZoneGeneration: req.ZoneGeneration,
		RequestedStart: req.Start, RequestedGoal: req.Goal, MaxVisitedPolygon: 2048, MaxProjectionDistance: zonenavigation.ProjectionDistance, Points: make([]scenario.WorldPosition, 0)}
	if ctx == nil || req.UserID <= 0 || req.GameID == 0 || req.PeerGeneration == 0 || req.TransportGeneration == 0 || req.ZoneGeneration == 0 || !isWorldPathPointFinite(req.Start) || !isWorldPathPointFinite(req.Goal) {
		return result, errors.New("world path requires context, owned game epochs and finite start/goal points")
	}
	captured, err := e.captureWorldPath(ctx, req)
	if err != nil {
		return result, fmt.Errorf("pathCapture: %w", err)
	}
	result.PlanLayer = captured.planLayer
	result.FootprintRadius = captured.footprintRadius
	err = ctx.Err()
	if err != nil {
		return result, fmt.Errorf("pathBeforeSearch: %w", err)
	}
	// CreatePath owns only local search state. Its fixed polygon/obstacle-node
	// bounds limit work, but it cannot be interrupted inside this call. Neither
	// registry nor zone locks are held while it runs; networking remains live.
	path, pathErr := captured.mesh.CreatePath(navigation.Vec3(req.Start), navigation.Vec3(req.Goal), navigation.PathOptions{
		ProjectionOptions: navigation.ProjectionOptions{PlanLayer: captured.planLayer, MaxDistance: zonenavigation.ProjectionDistance}, MaxVisitedPolygon: result.MaxVisitedPolygon,
	})
	err = ctx.Err()
	if err != nil {
		return result, fmt.Errorf("pathAfterSearch: %w", err)
	}
	current, err := e.captureWorldPath(ctx, req)
	if err != nil {
		return result, fmt.Errorf("pathEpochRecheck: %w", err)
	}
	if current.mesh != captured.mesh || current.objectID != captured.objectID || current.planLayer != captured.planLayer || current.footprintRadius != captured.footprintRadius {
		return result, errors.New("navigation actor or selected-layout mesh changed during path query")
	}
	if pathErr != nil {
		// A bounded geometry search finding no route is ordinary observation,
		// not a successful path or a proof that no larger search could succeed.
		result.UnavailableReason = fmt.Sprintf("bounded navigation search: %v", pathErr)
		return result, nil
	}
	result.Start = worldPathProjection(path.Start)
	result.Goal = worldPathProjection(path.Goal)
	result.PointTotal = len(path.Points)
	for index, point := range path.Points {
		if index >= 128 {
			break
		}
		result.Points = append(result.Points, scenario.WorldPosition(point))
	}
	if len(path.Points) > 128 {
		result.IsTruncated = true
		result.UnavailableReason = "navigation route exceeds 128 output points; returned prefix is not a complete path"
		return result, nil
	}
	if len(path.Points) < 2 || path.Start.ComponentID == 0 || path.Start.ComponentID != path.Goal.ComponentID {
		result.UnavailableReason = "navigation query returned no complete connected-component route"
		return result, nil
	}
	result.IsAvailable = true
	result.IsComplete = true
	return result, nil
}

func (e *gameplaySessionRegistry) captureWorldPath(ctx context.Context, req scenario.WorldPathRequest) (worldPathContext, error) {
	if e == nil {
		return worldPathContext{}, errors.New("gameplay registry is unavailable")
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !e.mutex.TryRLock() {
		select {
		case <-ctx.Done():
			return worldPathContext{}, fmt.Errorf("pathRegistryWait: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	defer e.mutex.RUnlock()
	err := ctx.Err()
	if err != nil {
		return worldPathContext{}, fmt.Errorf("pathRegistryContext: %w", err)
	}
	peer, observation, err := e.scenarioSessionLocked(ctx, req.UserID)
	if err != nil {
		return worldPathContext{}, fmt.Errorf("pathMember: %w", err)
	}
	if observation.Outcome != scenario.Passed || peer.binding.GameID != req.GameID || peer.generation != req.PeerGeneration || peer.transportGeneration != req.TransportGeneration || peer.zone == nil || !peer.dungeonSetup.IsCommitted() || !peer.stage.IsDungeon() || peer.deployedObjectID == 0 {
		return worldPathContext{}, errors.New("world path gameplay identity is unavailable or changed")
	}
	if peer.zone.Snapshot().Generation != req.ZoneGeneration {
		return worldPathContext{}, errors.New("world path zone generation changed")
	}
	mesh := peer.zone.Navigation()
	if mesh == nil {
		return worldPathContext{}, errors.New("installed selected-layout navigation is unavailable")
	}
	radius := peer.deployedCampaignNavigationRadius()
	actor := navigation.ActorNavigation{Mode: uint8(peer.binding.Mode)}
	if peer.playerMotion != nil {
		actor = peer.playerMotion.NavigationSettings()
		actor.Mode = uint8(peer.binding.Mode)
	}
	layer, isLayer := mesh.ActorLayer(radius, actor)
	if !isLayer {
		return worldPathContext{}, errors.New("deployed actor navigation layer is unavailable")
	}
	return worldPathContext{mesh: mesh, objectID: peer.deployedObjectID, planLayer: layer, footprintRadius: radius}, nil
}

func isWorldPathPointFinite(point scenario.WorldPosition) bool {
	return !math.IsNaN(float64(point.X)) && !math.IsInf(float64(point.X), 0) && !math.IsNaN(float64(point.Y)) && !math.IsInf(float64(point.Y), 0) && !math.IsNaN(float64(point.Z)) && !math.IsInf(float64(point.Z), 0)
}

func worldPathProjection(projection navigation.Projection) scenario.WorldPathProjection {
	return scenario.WorldPathProjection{Position: scenario.WorldPosition(projection.Position), PolygonID: projection.PolygonID, ComponentID: projection.ComponentID, Distance: projection.Distance}
}
