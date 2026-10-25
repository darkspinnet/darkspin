//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/scenario/control"
)

type debugPathPointRequest struct {
	X *float64 `json:"x"`
	Y *float64 `json:"y"`
	Z *float64 `json:"z"`
}

type debugPathProjection struct {
	Position    debugWorldPosition `json:"position"`
	PolygonID   uint32             `json:"polygon_id"`
	ComponentID uint32             `json:"component_id"`
	Distance    float32            `json:"distance"`
}

type debugWorldPath struct {
	ClientID              string               `json:"client_id"`
	InstanceID            string               `json:"instance_id"`
	PID                   uint32               `json:"pid"`
	UserID                int64                `json:"user_id"`
	GameID                uint32               `json:"game_id"`
	PeerGeneration        uint64               `json:"peer_generation"`
	TransportGeneration   uint64               `json:"transport_generation"`
	ZoneGeneration        uint64               `json:"zone_generation"`
	Source                string               `json:"source"`
	CapturedAt            time.Time            `json:"captured_at"`
	IsAvailable           bool                 `json:"is_available"`
	UnavailableReason     string               `json:"unavailable_reason,omitempty"`
	IsComplete            bool                 `json:"is_complete"`
	IsTruncated           bool                 `json:"is_truncated"`
	PlanLayer             uint8                `json:"plan_layer"`
	FootprintRadius       float32              `json:"footprint_radius"`
	MaxProjectionDistance float32              `json:"max_projection_distance"`
	MaxVisitedPolygon     int                  `json:"max_visited_polygon"`
	RequestedStart        debugWorldPosition   `json:"requested_start"`
	RequestedGoal         debugWorldPosition   `json:"requested_goal"`
	Start                 debugPathProjection  `json:"start"`
	Goal                  debugPathProjection  `json:"goal"`
	Points                []debugWorldPosition `json:"points"`
	PointTotal            int                  `json:"point_total"`
}

func queryDebugWorldPath(ctx context.Context, host *debugHost, clientManager *debugClientManager, payload json.RawMessage) (debugWorldPath, error) {
	result := debugWorldPath{Source: "server_navigation_path", Points: make([]debugWorldPosition, 0)}
	var req struct {
		ClientID string                 `json:"client_id"`
		Start    *debugPathPointRequest `json:"start"`
		Goal     *debugPathPointRequest `json:"goal"`
	}
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return result, fmt.Errorf("pathPayload: %w", err)
	}
	start, err := debugPathPoint(req.Start)
	if err != nil {
		return result, fmt.Errorf("pathStartPoint: %w", err)
	}
	goal, err := debugPathPoint(req.Goal)
	if err != nil {
		return result, fmt.Errorf("pathGoalPoint: %w", err)
	}
	owner, err := observeDebugWorldOwner(ctx, host, clientManager, req.ClientID)
	if err != nil {
		return result, fmt.Errorf("pathOwner: %w", err)
	}
	result.ClientID = owner.client.ClientID
	result.InstanceID = owner.client.InstanceID
	result.PID = owner.client.ProcessID
	result.UserID = owner.client.UserID
	result.RequestedStart = debugWorldPositionDTO(start)
	result.RequestedGoal = debugWorldPositionDTO(goal)
	if owner.profile.GameID == 0 {
		return result, errors.New("world path requires an admitted game")
	}
	world, err := host.server.DebugWorldState(ctx, owner.client.UserID, owner.profile.GameID)
	if err != nil {
		return result, fmt.Errorf("pathWorld: %w", err)
	}
	if !world.IsAvailable {
		return result, &control.ScenarioError{Code: control.NotAvailable, Context: "pathWorld", Message: world.UnavailableReason}
	}
	path, pathErr := host.server.DebugWorldPath(ctx, scenario.WorldPathRequest{UserID: owner.client.UserID, GameID: owner.profile.GameID, PeerGeneration: world.PeerGeneration, TransportGeneration: world.TransportGeneration, ZoneGeneration: world.ZoneGeneration, Start: start, Goal: goal})
	result = debugWorldPathDTO(owner, path)
	ownerErr := recheckDebugWorldOwner(ctx, host, clientManager, owner)
	if pathErr != nil || ownerErr != nil {
		return result, fmt.Errorf("pathQuery: %w", errors.Join(pathErr, ownerErr))
	}
	return result, nil
}

func debugPathPoint(point *debugPathPointRequest) (scenario.WorldPosition, error) {
	if point == nil || point.X == nil || point.Y == nil || point.Z == nil {
		return scenario.WorldPosition{}, errors.New("path point requires numeric x, y and z")
	}
	for _, coordinate := range []float64{*point.X, *point.Y, *point.Z} {
		if math.IsNaN(coordinate) || math.IsInf(coordinate, 0) || math.Abs(coordinate) > math.MaxFloat32 {
			return scenario.WorldPosition{}, errors.New("path point coordinate is not a finite game position")
		}
	}
	return scenario.WorldPosition{X: float32(*point.X), Y: float32(*point.Y), Z: float32(*point.Z)}, nil
}

func debugWorldPathDTO(owner debugWorldOwner, path scenario.WorldPath) debugWorldPath {
	result := debugWorldPath{ClientID: owner.client.ClientID, InstanceID: owner.client.InstanceID, PID: owner.client.ProcessID, UserID: owner.client.UserID, GameID: path.GameID, PeerGeneration: path.PeerGeneration, TransportGeneration: path.TransportGeneration, ZoneGeneration: path.ZoneGeneration,
		Source: "server_navigation_path", CapturedAt: path.CapturedAt, IsAvailable: path.IsAvailable, UnavailableReason: path.UnavailableReason, IsComplete: path.IsComplete, IsTruncated: path.IsTruncated, PlanLayer: path.PlanLayer, FootprintRadius: path.FootprintRadius, MaxProjectionDistance: path.MaxProjectionDistance, MaxVisitedPolygon: path.MaxVisitedPolygon,
		RequestedStart: debugWorldPositionDTO(path.RequestedStart), RequestedGoal: debugWorldPositionDTO(path.RequestedGoal), Start: debugPathProjectionDTO(path.Start), Goal: debugPathProjectionDTO(path.Goal), Points: make([]debugWorldPosition, 0, len(path.Points)), PointTotal: path.PointTotal}
	for _, point := range path.Points {
		result.Points = append(result.Points, debugWorldPositionDTO(point))
	}
	return result
}

func debugPathProjectionDTO(projection scenario.WorldPathProjection) debugPathProjection {
	return debugPathProjection{Position: debugWorldPositionDTO(projection.Position), PolygonID: projection.PolygonID, ComponentID: projection.ComponentID, Distance: projection.Distance}
}
