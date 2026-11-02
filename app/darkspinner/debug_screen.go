//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario/control"
)

// A selected browser overlay does not prove its remote content or DOM rendered.
type controlScreenState struct {
	IsAvailable           bool   `json:"is_available"`
	Screen                string `json:"screen"`
	IsInventoryOpen       bool   `json:"is_inventory_open"`
	IsBrowserOpen         bool   `json:"is_browser_open"`
	NativeScreenID        *int32 `json:"native_screen_id"`
	IsContentReady        *bool  `json:"is_content_ready"`
	OwnerGeneration       uint64 `json:"owner_generation"`
	IsInvocationAttempted bool   `json:"is_invocation_attempted"`
	IsInvocationReturned  bool   `json:"is_invocation_returned"`
	SelectionSource       string `json:"selection_source,omitempty"`
}

type debugScreenRequest struct {
	ClientID  string `json:"client_id"`
	Screen    string `json:"screen,omitempty"`
	TimeoutMS uint32 `json:"timeout_ms,omitempty"`
}

type debugScreenResult struct {
	ClientID          string              `json:"client_id"`
	InstanceID        string              `json:"instance_id"`
	PID               uint32              `json:"pid"`
	Action            any                 `json:"action"`
	Observation       any                 `json:"observation"`
	ScreenState       *controlScreenState `json:"screen_state"`
	IsAlreadySelected bool                `json:"is_already_selected"`
}

func isDebugScreen(screen string) bool {
	switch screen {
	case "none", "profile", "inventory", "friends", "leaderboards", "manual", "galaxy":
		return true
	default:
		return false
	}
}

func executeDebugScreen(ctx context.Context, host *debugHost, manager *debugClientManager, operation string, payload json.RawMessage) (debugScreenResult, error) {
	result := debugScreenResult{}
	var req debugScreenRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return result, fmt.Errorf("screenPayload: %w", err)
	}
	if operation != "client.screen_state" && operation != "client.set_screen" && operation != "client.wait_screen" {
		return result, errors.New("unsupported overlay screen operation")
	}
	if req.ClientID == "" || req.TimeoutMS > 120000 || operation == "client.screen_state" && req.Screen != "" || operation != "client.screen_state" && !isDebugScreen(req.Screen) {
		return result, errors.New("screen operation requires an owned client, an exact supported screen for Set/Wait and timeout up to 120000 milliseconds")
	}
	if req.TimeoutMS == 0 {
		req.TimeoutMS = 60000
	}
	waitContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	owner, err := observeDebugWorldOwner(waitContext, host, manager, req.ClientID)
	if err != nil {
		return result, fmt.Errorf("screenOwner: %w", debugScreenWaitFailure(waitContext, err))
	}
	result.ClientID, result.InstanceID, result.PID = owner.client.ClientID, owner.client.InstanceID, owner.client.ProcessID
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	isDispatched := false
	ownerGeneration := uint64(0)
	ownerThreadID := uint32(0)
	for {
		err = waitContext.Err()
		if err != nil {
			return result, fmt.Errorf("screenBeforeProbe: %w", debugScreenWaitFailure(waitContext, err))
		}
		err = recheckDebugWorldOwner(waitContext, host, manager, owner)
		if err != nil {
			return result, fmt.Errorf("screenIdentity: %w", debugScreenWaitFailure(waitContext, err))
		}
		result.Observation, err = manager.InspectScreen(waitContext, req.ClientID)
		if err != nil {
			return result, fmt.Errorf("screenProbe: %w", debugScreenWaitFailure(waitContext, err))
		}
		receipt, isReceipt := result.Observation.(map[string]any)
		if !isReceipt {
			return result, errors.New("native screen observation has an invalid receipt shape")
		}
		screenState, isScreen := receipt["screen_state"].(*controlScreenState)
		if !isScreen || screenState == nil {
			return result, errors.New("native screen observation omitted its typed state")
		}
		result.ScreenState = screenState
		threadID, isThread := receipt["thread_id"].(uint32)
		if !isThread || threadID == 0 {
			return result, errors.New("native screen observation omitted its owner thread")
		}
		if ownerGeneration != 0 && (screenState.OwnerGeneration != ownerGeneration || threadID != ownerThreadID) {
			return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "screenLifecycle", Message: "native UI owner or its processing thread changed during screen operation; any action is not replayed"}
		}
		if screenState.IsAvailable {
			if screenState.OwnerGeneration == 0 {
				return result, errors.New("available native screen observation omitted its lifecycle generation")
			}
			ownerGeneration, ownerThreadID = screenState.OwnerGeneration, threadID
		}
		err = recheckDebugWorldOwner(waitContext, host, manager, owner)
		if err != nil {
			return result, fmt.Errorf("screenAfterProbe: %w", debugScreenWaitFailure(waitContext, err))
		}
		if operation == "client.screen_state" {
			return result, nil
		}
		if screenState.IsAvailable && screenState.Screen == req.Screen {
			result.IsAlreadySelected = operation == "client.set_screen" && !isDispatched
			return result, nil
		}
		if operation == "client.set_screen" && !isDispatched && screenState.IsAvailable {
			if screenState.IsInventoryOpen && req.Screen != "none" {
				return result, &control.ScenarioError{Code: control.NotAvailable, Context: "screenInventory", Message: "close the current inventory through UI.Screen:Close before selecting another overlay"}
			}
			isDispatched = true
			result.Action, err = manager.SetScreen(waitContext, req.ClientID, req.Screen, ownerGeneration)
			if err != nil {
				return result, fmt.Errorf("screenDispatch: %w", debugScreenWaitFailure(waitContext, err))
			}
		}
		select {
		case <-waitContext.Done():
			return result, fmt.Errorf("screenWait: %w", debugScreenWaitFailure(waitContext, waitContext.Err()))
		case <-ticker.C:
		}
	}
}

func debugScreenWaitFailure(ctx context.Context, cause error) error {
	contextErr := ctx.Err()
	if contextErr == nil {
		return fmt.Errorf("screenCall: %w", cause)
	}
	code := control.DeadlineExceeded
	if errors.Is(contextErr, context.Canceled) {
		code = control.ErrorCode("cancelled")
	}
	if !errors.Is(cause, contextErr) {
		cause = errors.Join(contextErr, cause)
	}
	return &control.ScenarioError{Code: code, Context: "screenWait", Message: "screen observation ended before completion; any dispatched action is not replayed", Cause: cause}
}
