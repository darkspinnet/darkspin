//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Debugger suspension affects one owned client process, never the server clock.
type debugPauseState struct {
	RunID                string    `json:"run_id"`
	ProcessCreation      string    `json:"process_creation"`
	IsBridgeRecovered    bool      `json:"is_bridge_recovered"`
	IsFrameRecovered     bool      `json:"is_frame_recovered"`
	IsAccountRecovered   bool      `json:"is_account_recovered"`
	Scope                string    `json:"scope"`
	IsSimulationPaused   bool      `json:"is_simulation_paused"`
	StopEventThreadID    uint32    `json:"stop_event_thread_id"`
	StoppedAt            time.Time `json:"stopped_at,omitempty"`
	ClientID             string    `json:"client_id"`
	InstanceID           string    `json:"instance_id"`
	PID                  uint32    `json:"pid"`
	PauseID              string    `json:"pause_id"`
	Status               string    `json:"status"`
	IsPaused             bool      `json:"is_paused"`
	IsRecovered          bool      `json:"is_recovered"`
	StartedAt            time.Time `json:"started_at"`
	ExpiresAt            time.Time `json:"expires_at"`
	ResumedAt            time.Time `json:"resumed_at,omitempty"`
	PrePauseManifestPath string    `json:"pre_pause_manifest_path,omitempty"`
	Failure              string    `json:"failure,omitempty"`
}

type debugPauseRequest struct {
	ClientID   string `json:"client_id"`
	PauseID    string `json:"pause_id,omitempty"`
	DurationMS uint32 `json:"duration_ms,omitempty"`
}

func (e *debugSupervisor) executeDebugPause(ctx context.Context, host *debugHost, clientManager *debugClientManager, operation string, payload json.RawMessage) (debugPauseState, error) {
	var req debugPauseRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return debugPauseState{}, fmt.Errorf("pauseRequest: %w", err)
	}
	if clientManager == nil || host == nil {
		return debugPauseState{}, errors.New("debugger pause requires an owned running server and client")
	}
	if operation == "client.debug_pause_state" {
		if req.PauseID != "" || req.DurationMS != 0 {
			return debugPauseState{}, errors.New("debugger state accepts only client_id")
		}
		result, stateErr := clientManager.DebugPauseState(req.ClientID)
		if stateErr != nil {
			return result, fmt.Errorf("pauseState: %w", stateErr)
		}
		return result, nil
	}
	if operation == "client.debug_resume" {
		if req.DurationMS != 0 {
			return debugPauseState{}, errors.New("debugger resume accepts only client_id and pause_id")
		}
		result, resumeErr := clientManager.ResumeDebugPause(ctx, req.ClientID, req.PauseID)
		if resumeErr != nil {
			return result, fmt.Errorf("pauseResume: %w", resumeErr)
		}
		return result, nil
	}
	if req.DurationMS == 0 {
		req.DurationMS = 2000
	}
	if req.DurationMS > 5000 || req.PauseID != "" {
		return debugPauseState{}, errors.New("debugger pause requires 1 through 5000 milliseconds and no caller pause token")
	}
	owner, err := observeDebugWorldOwner(ctx, host, clientManager, req.ClientID)
	if err != nil {
		return debugPauseState{}, fmt.Errorf("pauseOwner: %w", err)
	}
	// Evidence is finalized before suspension. A partial diagnostic is not a
	// successful capture and must not authorize attaching a debugger.
	capturePayload, err := json.Marshal(debugDiagnosticRequest{ClientID: req.ClientID, Label: "pre-pause"})
	if err != nil {
		return debugPauseState{}, fmt.Errorf("pauseCaptureEncode: %w", err)
	}
	capture, err := e.captureDebugDiagnostic(ctx, host, clientManager, capturePayload)
	if err != nil {
		return debugPauseState{ClientID: req.ClientID, PrePauseManifestPath: capture.ManifestPath}, fmt.Errorf("pauseCapture: %w", err)
	}
	if !capture.IsComplete {
		return debugPauseState{ClientID: req.ClientID, PrePauseManifestPath: capture.ManifestPath}, errors.New("pre-pause diagnostic is incomplete; client was not suspended")
	}
	err = recheckDebugWorldOwner(ctx, host, clientManager, owner)
	if err != nil {
		return debugPauseState{}, fmt.Errorf("pauseRecheck: %w", err)
	}
	result, err := clientManager.StartDebugPause(ctx, host, owner, time.Duration(req.DurationMS)*time.Millisecond, capture.ManifestPath)
	if err != nil {
		return result, fmt.Errorf("pauseAttach: %w", err)
	}
	return result, nil
}
