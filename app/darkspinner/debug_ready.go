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

type debugClientReadyRequest struct {
	ClientID  string `json:"client_id"`
	TimeoutMS uint32 `json:"timeout_ms"`
}

type debugClientSceneRequest struct {
	ClientID      string `json:"client_id"`
	Location      string `json:"location"`
	TimeoutMS     uint32 `json:"timeout_ms"`
	AfterRevision uint64 `json:"after_revision"`
}

// Readiness is an actual owner-thread receipt, never a visual scene assertion.
// State and readiness are correlated sequential observations, not atomic state.
type debugClientReadiness struct {
	ClientID   string            `json:"client_id"`
	InstanceID string            `json:"instance_id"`
	PID        uint32            `json:"pid"`
	State      debugProfileState `json:"state"`
	Readiness  any               `json:"readiness"`
}

func waitDebugClientReady(ctx context.Context, host *debugHost, clientManager *debugClientManager, operation string, payload json.RawMessage) (debugClientReadiness, error) {
	if host == nil || clientManager == nil {
		return debugClientReadiness{}, errors.New("owned server and client manager are required for readiness")
	}
	var req debugClientSceneRequest
	if operation == "client.ready" {
		var readyReq debugClientReadyRequest
		err := decodeDebugPayload(payload, &readyReq)
		if err != nil {
			return debugClientReadiness{}, fmt.Errorf("readyRequest: %w", err)
		}
		req.ClientID = readyReq.ClientID
		req.TimeoutMS = readyReq.TimeoutMS
	} else {
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return debugClientReadiness{}, fmt.Errorf("sceneRequest: %w", err)
		}
		switch req.Location {
		case "arsenal", "navigation", "editor":
		default:
			return debugClientReadiness{}, errors.New("reported location must be arsenal, navigation, or editor")
		}
	}
	if req.ClientID == "" || req.TimeoutMS == 0 || req.TimeoutMS > 120000 {
		return debugClientReadiness{}, errors.New("readiness requires a client ID and a timeout from 1 to 120000 milliseconds")
	}
	waitContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	client, err := clientManager.State(req.ClientID)
	if err != nil {
		return debugClientReadiness{}, fmt.Errorf("readyClient: %w", err)
	}
	result := debugClientReadiness{ClientID: client.ClientID, InstanceID: client.InstanceID, PID: client.ProcessID}
	if client.ProfileName == "" || client.InstanceID == "" || client.ProcessID == 0 || !client.IsRunning || client.Status == "liveness_unavailable" {
		return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "readyIdentity", Message: "client does not identify a live owned process and profile"}
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		err = waitContext.Err()
		if err != nil {
			return result, debugClientReadyContext(err)
		}
		before, stateErr := host.ProfileState(waitContext, client.ProfileName)
		if stateErr != nil {
			return result, fmt.Errorf("readyProfile: %w", stateErr)
		}
		result.State = before
		if matchesDebugClientReady(before, req) {
			var readiness any
			var readyErr error
			if operation == "client.wait_scene" {
				readiness, readyErr = clientManager.ReadyScene(waitContext, req.ClientID, req.Location)
			} else {
				readiness, readyErr = clientManager.ReadyWindow(waitContext, req.ClientID)
			}
			result.Readiness = readiness
			if readyErr != nil {
				var failure *control.ScenarioError
				if operation == "client.wait_scene" && errors.As(readyErr, &failure) && failure.Code == control.NotAvailable {
					// This is a pure eligibility query, with no input or scene write.
					// An explicitly unavailable UI may settle under the same deadline.
					select {
					case <-waitContext.Done():
						return result, debugClientReadyContext(waitContext.Err())
					case <-ticker.C:
						continue
					}
				}
				// This probe may already have reached the owner thread. Preserve
				// its receipt/error; never replay it after an unknown outcome.
				return result, fmt.Errorf("readyWindow: %w", readyErr)
			}
			after, afterErr := host.ProfileState(waitContext, client.ProfileName)
			if afterErr != nil {
				return result, fmt.Errorf("readyRecheck: %w", afterErr)
			}
			result.State = after
			current, currentErr := clientManager.State(req.ClientID)
			if currentErr != nil {
				return result, fmt.Errorf("readyLiveness: %w", currentErr)
			}
			if current.InstanceID != client.InstanceID || current.ProcessID != client.ProcessID || current.ProfileName != client.ProfileName || !current.IsRunning || current.Status == "liveness_unavailable" {
				return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "readyRevalidate", Message: "owned client identity or liveness changed during the readiness probe", ClientInstanceID: client.InstanceID}
			}
			err = waitContext.Err()
			if err != nil {
				return result, debugClientReadyContext(err)
			}
			if matchesDebugClientReady(after, req) && after.UserID == before.UserID && after.SessionID == before.SessionID && (operation == "client.ready" || after.Presence.Revision == before.Presence.Revision) {
				return result, nil
			}
		}
		select {
		case <-waitContext.Done():
			return result, debugClientReadyContext(waitContext.Err())
		case <-ticker.C:
		}
	}
}

func matchesDebugClientReady(state debugProfileState, req debugClientSceneRequest) bool {
	if !state.IsAuthenticated {
		return false
	}
	if req.Location == "" {
		return true
	}
	return state.Presence.Status == "available" && state.Presence.Location == req.Location && state.Presence.Revision > req.AfterRevision
}

func debugClientReadyContext(err error) error {
	code := control.DeadlineExceeded
	if errors.Is(err, context.Canceled) {
		code = control.ErrorCode("cancelled")
	}
	return &control.ScenarioError{Code: code, Context: "clientReadiness", Message: "authenticated client readiness was not established within the requested lifetime", Cause: err}
}
