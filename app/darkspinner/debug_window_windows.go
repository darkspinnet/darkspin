//go:build scenario && windows

package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/scenario/control"
)

func (e *debugClientManager) InspectWindow(ctx context.Context, clientID string) (any, error) {
	return e.requestWindow(ctx, clientID, "window.inspect", controlCommand{})
}

func (e *debugClientManager) inspectCinematic(ctx context.Context, clientID string) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "cinematic.snapshot", controlCommand{})
	if err != nil {
		return result, fmt.Errorf("cinematicWindow: %w", err)
	}
	return result, nil
}

func (e *debugClientManager) InspectMap(ctx context.Context, clientID string) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "mission.map_state", controlCommand{})
	if err != nil {
		return result, fmt.Errorf("mapInspect: %w", err)
	}
	return result, nil
}

func (e *debugClientManager) CancelEditor(ctx context.Context, clientID string) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "editor.cancel", controlCommand{})
	if err != nil {
		return result, fmt.Errorf("editorCancel: %w", err)
	}
	return result, nil
}

// ReadyWindow confirms only that the actual window owner consumed a probe.
// It makes no assertion about menu, scene, loading, or gameplay readiness.
func (e *debugClientManager) ReadyWindow(ctx context.Context, clientID string) (any, error) {
	return e.requestWindow(ctx, clientID, "window.ready", controlCommand{})
}

func (e *debugClientManager) SetScene(ctx context.Context, clientID, scene string) (any, error) {
	return e.requestWindow(ctx, clientID, "scene.set", controlCommand{Scene: scene})
}

func (e *debugClientManager) ReadyScene(ctx context.Context, clientID, scene string) (any, error) {
	return e.requestWindow(ctx, clientID, "scene.ready", controlCommand{Scene: scene})
}

func (e *debugClientManager) ClickWindow(ctx context.Context, req debugWindowClickRequest) (any, error) {
	return e.requestWindow(ctx, req.ClientID, "window.click", controlCommand{X: req.X, Y: req.Y, Button: req.Button})
}

func (e *debugClientManager) PressWindow(ctx context.Context, req debugWindowPressRequest) (any, error) {
	return e.requestWindow(ctx, req.ClientID, "window.press", controlCommand{Key: req.Key})
}

func (e *debugClientManager) TypeWindow(ctx context.Context, req debugWindowTextRequest) (any, error) {
	return e.requestWindow(ctx, req.ClientID, "window.text", controlCommand{Text: req.Text})
}

func (e *debugClientManager) ConfigureCinematic(ctx context.Context, req debugCinematicRequest) (any, error) {
	err := validateDebugCinematic(req)
	if err != nil {
		return nil, fmt.Errorf("cinematicRequest: %w", err)
	}
	return e.requestWindow(ctx, req.ClientID, "cinematic.configure", controlCommand{IsIntroSkipped: req.IsIntroSkipped, IsCinematicSkipped: req.IsCinematicSkipped})
}

func (e *debugClientManager) requestWindow(ctx context.Context, clientID, operation string, command controlCommand) (any, error) {
	state, err := e.State(clientID)
	if err != nil {
		return nil, fmt.Errorf("windowClient: %w", err)
	}
	if !state.IsRunning || state.Status == "liveness_unavailable" {
		return nil, errors.New("owned client is not running or its liveness cannot be established")
	}
	e.mutex.Lock()
	client := e.clientsByID[clientID]
	e.mutex.Unlock()
	// Parallel scenario observations share the same single-command pipe with
	// native actions. Serialize individual requests, not the whole cast wait,
	// so screenshots and camera samples can continue between observations.
	err = lockScenarioLiveness(ctx, &client.windowCommandMutex)
	if err != nil {
		return nil, fmt.Errorf("windowCommandLock: %w", err)
	}
	defer client.windowCommandMutex.Unlock()
	client.mutex.Lock()
	bridge := client.bridge
	client.mutex.Unlock()
	if bridge == nil {
		return nil, errors.New("owned client bridge is unavailable")
	}
	receipt, err := bridge.Request(ctx, operation, command)
	result := map[string]any{"client_id": state.ClientID, "profile_name": state.ProfileName, "instance_id": state.InstanceID, "pid": state.ProcessID,
		"status": receipt.Status, "is_processed": receipt.IsProcessed, "is_gameplay_complete": receipt.IsGameplayComplete,
		"thread_id": receipt.ThreadID, "completed_tick_ms": receipt.CompletedTickMS}
	if receipt.Window != nil {
		window := receipt.Window
		if window.IsAvailable && window.ProcessID != state.ProcessID {
			return result, errors.New("window response does not belong to the retained client process")
		}
		result["window"] = map[string]any{"is_available": window.IsAvailable, "process_id": window.ProcessID, "thread_id": window.ThreadID,
			"width": window.Width, "height": window.Height, "is_enabled": window.IsEnabled, "is_minimized": window.IsMinimized, "window_id": window.WindowID}
	}
	if receipt.IsIntroSkipped != nil {
		result["is_intro_skipped"] = *receipt.IsIntroSkipped
	}
	if receipt.CinematicSnapshot != nil {
		result["cinematic_snapshot"] = receipt.CinematicSnapshot
	}
	if receipt.ProcessedCharacterCount != nil {
		result["processed_character_count"] = *receipt.ProcessedCharacterCount
	}
	if receipt.MapState != nil {
		result["map_state"] = receipt.MapState
	}
	if receipt.ScreenState != nil {
		result["screen_state"] = receipt.ScreenState
	}
	if receipt.EditorHierarchy != nil {
		result["editor_hierarchy"] = receipt.EditorHierarchy
	}
	if receipt.GameplayState != nil {
		result["gameplay_state"] = receipt.GameplayState
	}
	if receipt.HeroSelection != nil {
		result["hero_selection"] = receipt.HeroSelection
	}
	if receipt.ActiveCast != nil {
		result["active_cast"] = receipt.ActiveCast
	}
	if receipt.Projection != nil {
		result["projection"] = receipt.Projection
	}
	if receipt.GameplayClick != nil {
		result["gameplay_click"] = receipt.GameplayClick
	}
	if receipt.ArenaLobby != nil {
		result["arena_lobby"] = receipt.ArenaLobby
	}
	if receipt.DeploymentState != nil {
		result["deployment_state"] = receipt.DeploymentState
	}
	if receipt.BeamOutState != nil {
		result["beam_out_state"] = receipt.BeamOutState
	}
	if receipt.IsInvocationAttempted != nil {
		result["is_invocation_attempted"] = *receipt.IsInvocationAttempted
	}
	if receipt.IsInvocationAccepted != nil {
		result["is_invocation_accepted"] = *receipt.IsInvocationAccepted
	}
	if receipt.IsCinematicSkipped != nil {
		result["is_cinematic_skipped"] = *receipt.IsCinematicSkipped
	}
	if err != nil {
		var requestErr *controlRequestError
		if errors.As(err, &requestErr) {
			return result, &control.ScenarioError{Code: control.ErrorCode(requestErr.Code), Context: "windowRequest", Message: requestErr.Message, ClientInstanceID: state.InstanceID, ActionID: requestErr.RequestID, Cause: err}
		}
		return result, fmt.Errorf("windowCommand: %w", err)
	}
	return result, nil
}
