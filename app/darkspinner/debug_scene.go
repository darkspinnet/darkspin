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

type debugSceneRequest struct {
	ClientID  string `json:"client_id"`
	Location  string `json:"location"`
	TimeoutMS uint32 `json:"timeout_ms"`
}

type debugSceneTransition struct {
	Location       string               `json:"location"`
	IsAlreadyThere bool                 `json:"is_already_there"`
	Action         any                  `json:"action"`
	Observation    debugClientReadiness `json:"observation"`
}

func isDebugShipScene(location string) bool {
	return location == "arsenal" || location == "navigation" || location == "editor"
}

// Set is a requested native transition followed by observation, never a write
// to native scene pointers or an assertion that a room has finished rendering.
func setDebugClientScene(ctx context.Context, host *debugHost, clientManager *debugClientManager, payload json.RawMessage) (debugSceneTransition, error) {
	if host == nil || clientManager == nil {
		return debugSceneTransition{}, errors.New("scene setting requires an owned server and client manager")
	}
	var req debugSceneRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return debugSceneTransition{}, fmt.Errorf("sceneRequest: %w", err)
	}
	result := debugSceneTransition{Location: req.Location}
	if req.ClientID == "" || !isDebugShipScene(req.Location) || req.TimeoutMS == 0 || req.TimeoutMS > 120000 {
		return result, errors.New("scene setting requires a client ID, supported ship scene, and timeout from 1 to 120000 milliseconds")
	}
	waitContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	client, err := clientManager.State(req.ClientID)
	if err != nil {
		return result, fmt.Errorf("sceneClient: %w", err)
	}
	if !client.IsRunning || client.Status == "liveness_unavailable" || client.ProfileName == "" {
		return result, errors.New("scene setting requires a live owned client and profile")
	}
	before, err := host.ProfileState(waitContext, client.ProfileName)
	if err != nil {
		return result, fmt.Errorf("sceneBefore: %w", err)
	}
	result.Observation = debugClientReadiness{ClientID: client.ClientID, InstanceID: client.InstanceID, PID: client.ProcessID, State: before}
	if !before.IsAuthenticated {
		return result, errors.New("scene setting requires accepted client authentication")
	}
	afterRevision := before.Presence.Revision
	result.IsAlreadyThere = before.Presence.Status == "available" && before.Presence.Location == req.Location
	if result.IsAlreadyThere {
		afterRevision = 0
	} else {
		if before.Presence.Status != "available" || !isDebugShipScene(before.Presence.Location) {
			return result, errors.New("scene setting requires a known current ship scene")
		}
		prepared, prepareErr := waitDebugSceneObservation(waitContext, host, clientManager, debugClientSceneRequest{
			ClientID: req.ClientID, Location: before.Presence.Location, TimeoutMS: req.TimeoutMS,
		})
		result.Observation = prepared
		if prepareErr != nil {
			return result, fmt.Errorf("sceneEligible: %w", prepareErr)
		}
		if prepared.InstanceID != client.InstanceID || prepared.PID != client.ProcessID || prepared.State.SessionID != before.SessionID {
			return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "scenePrepare", Message: "client or accepted login changed before scene request"}
		}
		var action any
		var actionErr error
		if before.Presence.Location == "editor" {
			if req.Location != "arsenal" {
				return result, errors.New("leave Editor for Arsenal before requesting another ship scene")
			}
			action, actionErr = clientManager.CancelEditor(waitContext, req.ClientID)
		} else {
			action, actionErr = clientManager.SetScene(waitContext, req.ClientID, req.Location)
		}
		result.Action = action
		if actionErr != nil {
			return result, fmt.Errorf("sceneAction: %w", actionErr)
		}
	}
	observation, err := waitDebugSceneObservation(waitContext, host, clientManager, debugClientSceneRequest{ClientID: req.ClientID, Location: req.Location, TimeoutMS: req.TimeoutMS, AfterRevision: afterRevision})
	result.Observation = observation
	if err != nil {
		return result, fmt.Errorf("sceneWait: %w", err)
	}
	if observation.InstanceID != client.InstanceID || observation.PID != client.ProcessID || observation.State.SessionID != before.SessionID {
		return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "sceneRecheck", Message: "client or accepted login changed during scene transition"}
	}
	return result, nil
}

func waitDebugSceneObservation(ctx context.Context, host *debugHost, clientManager *debugClientManager, req debugClientSceneRequest) (debugClientReadiness, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return debugClientReadiness{}, fmt.Errorf("sceneEncode: %w", err)
	}
	observation, err := waitDebugClientReady(ctx, host, clientManager, "client.wait_scene", payload)
	if err != nil {
		return observation, fmt.Errorf("sceneObserve: %w", err)
	}
	return observation, nil
}
