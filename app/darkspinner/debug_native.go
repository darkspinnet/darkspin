//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

type controlWorldPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type controlGameplayState struct {
	IsAvailable      bool                   `json:"is_available"`
	IsWorldActive    bool                   `json:"is_world_active"`
	ControlledHandle *uint32                `json:"controlled_handle"`
	PickedHandle     *uint32                `json:"picked_handle"`
	Position         *controlWorldPoint     `json:"position"`
	Camera           *controlCameraSnapshot `json:"camera"`
	FrameSequence    uint32                 `json:"frame_sequence"`
	OwnerThreadID    uint32                 `json:"owner_thread_id"`
}

type controlCameraSnapshot struct {
	IsAvailable          bool               `json:"is_available"`
	IsOverrideActive     bool               `json:"is_override_active"`
	TargetHandle         uint32             `json:"target_handle"`
	Mode                 uint32             `json:"mode"`
	FollowMode           uint32             `json:"follow_mode"`
	ZoomMode             uint32             `json:"zoom_mode"`
	Center               *controlWorldPoint `json:"center"`
	DesiredPosition      *controlWorldPoint `json:"desired_position"`
	AnchorPosition       *controlWorldPoint `json:"anchor_position"`
	OffsetAnchorPosition *controlWorldPoint `json:"offset_anchor_position"`
	IsAnchorAvailable    bool               `json:"is_anchor_available"`
	SourcePosition       *controlWorldPoint `json:"source_position"`
	AnchorSource         string             `json:"anchor_source"`
	ControlMode          uint32             `json:"control_mode"`
	HoldTimer            *float64           `json:"hold_timer"`
	Distance             *float64           `json:"distance"`
	DistanceTarget       *float64           `json:"distance_target"`
}

type controlProjection struct {
	IsAvailable  bool     `json:"is_available"`
	IsOnScreen   bool     `json:"is_on_screen"`
	X            *float64 `json:"x"`
	Y            *float64 `json:"y"`
	ClientWidth  uint32   `json:"client_width"`
	ClientHeight uint32   `json:"client_height"`
}

type controlGameplayClick struct {
	IsNativeGroundQuerySuccess bool               `json:"is_native_ground_query_success"`
	RayPoint                   *controlWorldPoint `json:"ray_point"`
	ChosenGroundPoint          *controlWorldPoint `json:"chosen_ground_point"`
	IsProjected                bool               `json:"is_projected"`
	IsPressPollObserved        bool               `json:"is_press_poll_observed"`
	IsReleasePollObserved      bool               `json:"is_release_poll_observed"`
	RetainedHandle             *uint32            `json:"retained_handle"`
}

func isDebugWorldPointFinite(point controlWorldPoint) bool {
	return !math.IsNaN(point.X) && !math.IsInf(point.X, 0) && math.Abs(point.X) <= math.MaxFloat32 &&
		!math.IsNaN(point.Y) && !math.IsInf(point.Y, 0) && math.Abs(point.Y) <= math.MaxFloat32 &&
		!math.IsNaN(point.Z) && !math.IsInf(point.Z, 0) && math.Abs(point.Z) <= math.MaxFloat32
}

func executeDebugNative(ctx context.Context, clientManager *debugClientManager, operation string, payload json.RawMessage) (any, error) {
	if clientManager == nil {
		return nil, errors.New("native observation requires an owned client")
	}
	var req struct {
		ClientID string             `json:"client_id"`
		Point    *controlWorldPoint `json:"point,omitempty"`
		HoldMS   uint32             `json:"hold_ms,omitempty"`
	}
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return nil, fmt.Errorf("nativeRequest: %w", err)
	}
	if operation == "client.hold_world" {
		if req.HoldMS == 0 || req.HoldMS > 1200 {
			return nil, errors.New("world hold requires hold_ms from 1 through 1200")
		}
	} else if req.HoldMS != 0 {
		return nil, errors.New("hold_ms is supported only by world hold")
	}
	if operation == "client.native_state" {
		if req.Point != nil {
			return nil, errors.New("native state accepts only client_id")
		}
		result, inspectErr := clientManager.InspectGameplay(ctx, req.ClientID)
		if inspectErr != nil {
			return result, fmt.Errorf("nativeInspect: %w", inspectErr)
		}
		return result, nil
	}
	if (operation != "client.project_world" && operation != "client.click_world" && operation != "client.hold_world") || req.Point == nil || !isDebugWorldPointFinite(*req.Point) {
		return nil, errors.New("world action requires a finite point")
	}
	if operation == "client.hold_world" {
		result, holdErr := clientManager.HoldWorld(ctx, req.ClientID, *req.Point, req.HoldMS)
		if holdErr != nil {
			return result, fmt.Errorf("nativeHold: %w", holdErr)
		}
		return result, nil
	}
	if operation == "client.click_world" {
		result, clickErr := clientManager.ClickWorld(ctx, req.ClientID, *req.Point)
		if clickErr != nil {
			return result, fmt.Errorf("nativeClick: %w", clickErr)
		}
		return result, nil
	}
	result, projectErr := clientManager.ProjectWorld(ctx, req.ClientID, *req.Point)
	if projectErr != nil {
		return result, fmt.Errorf("nativeProject: %w", projectErr)
	}
	return result, nil
}
