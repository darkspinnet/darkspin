//go:build windows && cgo && scenario && fangdebug

package main

/*
#include "control.h"
*/
import "C"

import (
	"math"
	"time"
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

func controlCameraNumber(number C.float) *float64 {
	coordinate := float64(number)
	if math.IsNaN(coordinate) || math.IsInf(coordinate, 0) {
		return nil
	}
	return &coordinate
}

func controlCameraPoint(fields, mask uint32, coordinates [3]C.float) *controlWorldPoint {
	if fields&mask == 0 {
		return nil
	}
	return &controlWorldPoint{X: float64(coordinates[0]), Y: float64(coordinates[1]), Z: float64(coordinates[2])}
}

func controlCameraReceipt(snapshot C.fang_control_camera_snapshot) *controlCameraSnapshot {
	cameraSS := &controlCameraSnapshot{IsAvailable: snapshot.is_available != 0}
	if !cameraSS.IsAvailable {
		return cameraSS
	}
	cameraSS.IsOverrideActive = snapshot.is_override_active != 0
	cameraSS.TargetHandle = uint32(snapshot.target_handle)
	cameraSS.Mode = uint32(snapshot.mode)
	cameraSS.FollowMode = uint32(snapshot.follow_mode)
	cameraSS.ZoomMode = uint32(snapshot.zoom_mode)
	fields := uint32(snapshot.fields)
	cameraSS.Center = controlCameraPoint(fields, 1, snapshot.center)
	cameraSS.DesiredPosition = controlCameraPoint(fields, 2, snapshot.desired)
	cameraSS.AnchorPosition = controlCameraPoint(fields, 4, snapshot.anchor)
	cameraSS.OffsetAnchorPosition = controlCameraPoint(fields, 8, snapshot.offset_anchor)
	cameraSS.IsAnchorAvailable = snapshot.is_anchor_available != 0
	if cameraSS.IsAnchorAvailable {
		cameraSS.SourcePosition = controlCameraPoint(1, 1, snapshot.source)
		switch snapshot.anchor_source {
		case 1:
			cameraSS.AnchorSource = "control"
		case 2:
			cameraSS.AnchorSource = "render"
		case 3:
			cameraSS.AnchorSource = "object"
		}
	}
	cameraSS.ControlMode = uint32(snapshot.control_mode)
	cameraSS.HoldTimer = controlCameraNumber(snapshot.hold_timer)
	cameraSS.Distance = controlCameraNumber(snapshot.distance)
	cameraSS.DistanceTarget = controlCameraNumber(snapshot.distance_target)
	return cameraSS
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
	IsProjected                bool               `json:"is_projected"`
	IsPressPollObserved        bool               `json:"is_press_poll_observed"`
	IsReleasePollObserved      bool               `json:"is_release_poll_observed"`
	RetainedHandle             *uint32            `json:"retained_handle"`
	IsNativeGroundQuerySuccess bool               `json:"is_native_ground_query_success"`
	RayPoint                   *controlWorldPoint `json:"ray_point"`
	ChosenGroundPoint          *controlWorldPoint `json:"chosen_ground_point"`
}

// Acceptance confirms ordinary slot-edge dispatch only. The host must observe
// both the changed native handle and authoritative server slot separately.
type controlHeroSelection struct {
	RequestedSlot            uint32  `json:"requested_slot"`
	IsQueryProcessed         bool    `json:"is_query_processed"`
	PreviousControlledHandle *uint32 `json:"previous_controlled_handle"`
	ObservedControlledHandle *uint32 `json:"observed_controlled_handle"`
	PreviousSlot             *uint32 `json:"previous_slot"`
	ObservedSlot             *uint32 `json:"observed_slot"`
	EligibilityCode          *uint32 `json:"eligibility_code"`
}

type controlActiveCast struct {
	ActionIndex                uint32             `json:"action_index"`
	IsQueryProcessed           bool               `json:"is_query_processed"`
	IsNativeRequestAttempted   bool               `json:"is_native_request_attempted"`
	NativeResultCode           *int32             `json:"native_result_code"`
	AbilityID                  *uint32            `json:"ability_id"`
	TargetHandle               *uint32            `json:"target_handle"`
	IsNativeGroundQuerySuccess bool               `json:"is_native_ground_query_success"`
	RayPoint                   *controlWorldPoint `json:"ray_point"`
	ChosenGroundPoint          *controlWorldPoint `json:"chosen_ground_point"`
}

func isControlPointFinite(point controlWorldPoint) bool {
	return !math.IsNaN(point.X) && !math.IsInf(point.X, 0) && math.Abs(point.X) <= math.MaxFloat32 &&
		!math.IsNaN(point.Y) && !math.IsInf(point.Y, 0) && math.Abs(point.Y) <= math.MaxFloat32 &&
		!math.IsNaN(point.Z) && !math.IsInf(point.Z, 0) && math.Abs(point.Z) <= math.MaxFloat32
}

func (e *controlConnection) gameplayCommand(req controlEnvelope, response controlEnvelope, deadline time.Time) controlEnvelope {
	isClick := req.Operation == "gameplay.click"
	isHold := req.Operation == "gameplay.hold"
	isSelect := req.Operation == "gameplay.select_hero"
	isCast := req.Operation == "gameplay.cast_active"
	isCancel := req.Operation == "gameplay.cancel_ability"
	if isCast && (req.Command == nil || req.Command.AbilityID == 0) ||
		!isCast && req.Command != nil && req.Command.AbilityID != 0 {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "ability_id is required only for gameplay.cast_active"}
		return response
	}
	isProject := req.Operation == "gameplay.project" || isClick || isHold || isCast
	if req.Sequence > uint64(^uint32(0)) || (req.Command != nil && (req.Command.Key != "" || req.Command.Scene != "" ||
		req.Command.X != 0 || req.Command.Y != 0 || req.Command.Button != "" || req.Command.IsIntroSkipped != nil || req.Command.IsCinematicSkipped != nil)) {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "gameplay observation accepts only a bounded sequence and an optional projection point"}
		return response
	}
	var point controlWorldPoint
	var kind uint32
	var holdMS uint32
	var heroSlot uint32
	var abilityID uint32
	if isCancel {
		kind = 6
	}
	if isCast {
		abilityID = req.Command.AbilityID
	}
	if isSelect {
		if req.Command == nil || req.Command.HeroSlot == nil || *req.Command.HeroSlot > 2 {
			response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "hero selection requires hero_slot from 0 through 2"}
			return response
		}
		heroSlot = *req.Command.HeroSlot
		kind = 4
	}
	if isHold {
		if req.Command == nil || req.Command.HoldMS < 1 || req.Command.HoldMS > 1200 {
			response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "gameplay hold requires hold_ms from 1 through 1200"}
			return response
		}
		holdMS = req.Command.HoldMS
	}
	if isProject {
		if req.Command == nil || req.Command.WorldPoint == nil || !isControlPointFinite(*req.Command.WorldPoint) {
			response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "projection requires a finite float32-representable world point"}
			return response
		}
		point = *req.Command.WorldPoint
		kind = 1
		if isClick {
			kind = 2
		}
		if isHold {
			kind = 3
		}
		if isCast {
			kind = 5
		}
	}
	nativeID := uint32(req.Sequence)
	e.nativeMutex.Lock()
	if e.isDisconnected || (req.Sequence == e.lastCanceledSequence && req.RequestID == e.lastCanceledRequestID) {
		e.nativeMutex.Unlock()
		response.Result.Status = "canceled"
		response.Failure = &controlCommandFailure{Code: "canceled", Message: "gameplay observation canceled before submission"}
		return response
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		e.nativeMutex.Unlock()
		response.Result.Status = "expired"
		response.Failure = &controlCommandFailure{Code: "deadline_exceeded", Message: "gameplay observation expired before submission"}
		return response
	}
	timeoutMS := uint32((remaining + time.Millisecond - 1) / time.Millisecond)
	isAccepted := C.fang_control_gameplay_submit(C.uint32_t(nativeID), C.uint32_t(kind), C.float(point.X), C.float(point.Y), C.float(point.Z), C.uint32_t(timeoutMS), C.uint32_t(holdMS), C.uint32_t(heroSlot), C.uint32_t(abilityID))
	if isAccepted != 0 {
		e.activeNativeID = nativeID
		e.activeRequestID = req.RequestID
	}
	e.nativeMutex.Unlock()
	if isAccepted == 0 {
		response.Result.Status = "unavailable"
		response.Failure = &controlCommandFailure{Code: "unavailable", Message: "validated gameplay frame observation queue is unavailable"}
		return response
	}
	defer e.clearNativeCommand(nativeID)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	for {
		var receipt C.fang_control_window_receipt
		var snapshot C.fang_control_gameplay_state
		var projection C.fang_control_projection
		var click C.fang_control_gameplay_click
		var selection C.fang_control_hero_selection
		var cast C.fang_control_active_cast
		isFound := C.fang_control_gameplay_result(C.uint32_t(nativeID), &receipt, &snapshot, &projection, &click, &selection, &cast)
		if isFound != 0 && receipt.status != 0 {
			return controlGameplayReceipt(response, receipt, snapshot, projection, click, selection, cast, deadline)
		}
		select {
		case <-e.done:
			C.fang_control_gameplay_cancel(C.uint32_t(nativeID))
			response.Result.Status = "canceled"
			response.Failure = &controlCommandFailure{Code: "canceled", Message: "control peer disconnected during gameplay observation"}
		case <-timer.C:
			C.fang_control_gameplay_cancel(C.uint32_t(nativeID))
			response.Result.Status = "expired"
			response.Failure = &controlCommandFailure{Code: "deadline_exceeded", Message: "gameplay frame observation deadline exceeded"}
		case <-ticker.C:
			continue
		}
		isFound = C.fang_control_gameplay_result(C.uint32_t(nativeID), &receipt, &snapshot, &projection, &click, &selection, &cast)
		if isFound != 0 && receipt.status != 0 {
			terminalStatus, terminalFailure := response.Result.Status, response.Failure
			response = controlGameplayReceipt(response, receipt, snapshot, projection, click, selection, cast, deadline)
			if receipt.status == 1 {
				response.Result.Status, response.Failure = terminalStatus, terminalFailure
			}
		}
		return response
	}
}

func controlGameplayReceipt(response controlEnvelope, receipt C.fang_control_window_receipt, snapshot C.fang_control_gameplay_state, projection C.fang_control_projection, click C.fang_control_gameplay_click, selection C.fang_control_hero_selection, cast C.fang_control_active_cast, deadline time.Time) controlEnvelope {
	response = controlWindowReceipt(response, receipt, deadline)
	state := &controlGameplayState{IsAvailable: snapshot.is_available != 0, IsWorldActive: snapshot.is_world_active != 0,
		Camera:           controlCameraReceipt(snapshot.camera),
		ControlledHandle: controlMapNumber(uint32(snapshot.fields), 1, uint32(snapshot.controlled_handle)),
		PickedHandle:     controlMapNumber(uint32(snapshot.fields), 4, uint32(snapshot.picked_handle)),
		FrameSequence:    uint32(snapshot.frame_sequence), OwnerThreadID: uint32(snapshot.owner_thread_id)}
	if snapshot.fields&2 != 0 {
		state.Position = &controlWorldPoint{X: float64(snapshot.x), Y: float64(snapshot.y), Z: float64(snapshot.z)}
	}
	response.Result.GameplayState = state
	if response.Operation == "gameplay.cancel_ability" {
		isAttempted := receipt.status == 1
		response.Result.IsInvocationAttempted = &isAttempted
		response.Result.IsInvocationAccepted = nil
	}
	if response.Operation == "gameplay.cast_active" {
		active := &controlActiveCast{ActionIndex: uint32(cast.action_index), IsQueryProcessed: cast.is_query_processed != 0,
			IsNativeRequestAttempted:   cast.is_native_request_attempted != 0,
			AbilityID:                  controlMapNumber(uint32(cast.fields), 2, uint32(cast.ability_id)),
			TargetHandle:               controlMapNumber(uint32(cast.fields), 4, uint32(cast.target_handle)),
			IsNativeGroundQuerySuccess: cast.ground.is_native_ground_query_success != 0}
		if cast.fields&1 != 0 {
			code := int32(cast.native_result_code)
			active.NativeResultCode = &code
		}
		if cast.ground.fields&2 != 0 {
			active.RayPoint = &controlWorldPoint{X: float64(cast.ground.ray_x), Y: float64(cast.ground.ray_y), Z: float64(cast.ground.ray_z)}
		}
		if cast.ground.fields&4 != 0 {
			active.ChosenGroundPoint = &controlWorldPoint{X: float64(cast.ground.ground_x), Y: float64(cast.ground.ground_y), Z: float64(cast.ground.ground_z)}
		}
		response.Result.ActiveCast = active
		isAttempted := cast.is_invocation_attempted != 0
		isAccepted := receipt.status == 1 && active.IsQueryProcessed && active.IsNativeRequestAttempted
		response.Result.IsInvocationAttempted = &isAttempted
		response.Result.IsInvocationAccepted = &isAccepted
		if receipt.status == 6 {
			response.Failure = &controlCommandFailure{Code: "outcome_unknown", Message: "active ability edge was dispatched without a completed native receipt; do not replay"}
		}
	}
	if response.Operation == "gameplay.select_hero" {
		fields := uint32(selection.fields)
		response.Result.HeroSelection = &controlHeroSelection{RequestedSlot: uint32(selection.requested_slot),
			IsQueryProcessed:         selection.is_query_processed != 0,
			PreviousControlledHandle: controlMapNumber(fields, 1, uint32(selection.previous_controlled_handle)),
			ObservedControlledHandle: controlMapNumber(fields, 2, uint32(selection.observed_controlled_handle)),
			EligibilityCode:          controlMapNumber(fields, 4, uint32(selection.eligibility_code)),
			PreviousSlot:             controlMapNumber(fields, 8, uint32(selection.previous_slot)),
			ObservedSlot:             controlMapNumber(fields, 16, uint32(selection.observed_slot))}
		isAttempted := selection.is_invocation_attempted != 0
		isAccepted := receipt.status == 1 && selection.is_query_processed != 0
		response.Result.IsInvocationAttempted = &isAttempted
		response.Result.IsInvocationAccepted = &isAccepted
		if receipt.status == 6 {
			response.Failure = &controlCommandFailure{Code: "outcome_unknown", Message: "ordinary hero slot edge was dispatched without a completed query receipt; do not replay"}
		}
	}
	if response.Operation == "gameplay.project" || response.Operation == "gameplay.click" || response.Operation == "gameplay.hold" || response.Operation == "gameplay.cast_active" {
		projected := &controlProjection{IsAvailable: projection.is_available != 0, IsOnScreen: projection.is_on_screen != 0,
			ClientWidth: uint32(projection.client_width), ClientHeight: uint32(projection.client_height)}
		if projected.IsAvailable {
			x, y := float64(projection.x), float64(projection.y)
			projected.X, projected.Y = &x, &y
		}
		response.Result.Projection = projected
	}
	if response.Operation == "gameplay.click" || response.Operation == "gameplay.hold" {
		clicked := &controlGameplayClick{IsProjected: click.is_projected != 0,
			IsPressPollObserved: click.is_press_poll_observed != 0, IsReleasePollObserved: click.is_release_poll_observed != 0,
			IsNativeGroundQuerySuccess: click.is_native_ground_query_success != 0,
			RetainedHandle:             controlMapNumber(uint32(click.fields), 1, uint32(click.retained_handle))}
		if click.fields&2 != 0 {
			clicked.RayPoint = &controlWorldPoint{X: float64(click.ray_x), Y: float64(click.ray_y), Z: float64(click.ray_z)}
		}
		if click.fields&4 != 0 {
			clicked.ChosenGroundPoint = &controlWorldPoint{X: float64(click.ground_x), Y: float64(click.ground_y), Z: float64(click.ground_z)}
		}
		response.Result.GameplayClick = clicked
		if receipt.status == 6 {
			response.Failure = &controlCommandFailure{Code: "outcome_unknown", Message: "native gameplay press was consumed without a witnessed completed release; do not replay"}
		}
	}
	return response
}
