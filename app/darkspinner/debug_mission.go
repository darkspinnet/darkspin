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

type debugMissionRequest struct {
	ClientID  string `json:"client_id"`
	TimeoutMS uint32 `json:"timeout_ms,omitempty"`
}

// This evidence describes the server's gameplay binding and lifecycle stage.
// It does not assert that the native client rendered the world or accepts input.
type debugMissionObservation struct {
	World      *debugWorldState  `json:"world,omitempty"`
	ClientID   string            `json:"client_id"`
	InstanceID string            `json:"instance_id"`
	PID        uint32            `json:"pid"`
	State      debugProfileState `json:"state"`
}

func observeDebugMission(ctx context.Context, host *debugHost, clientManager *debugClientManager, operation string, payload json.RawMessage) (debugMissionObservation, error) {
	switch operation {
	case "client.mission_state", "client.wait_mission", "client.wait_deployment":
	default:
		return debugMissionObservation{}, errors.New("unsupported mission observation")
	}
	if ctx == nil || host == nil || clientManager == nil {
		return debugMissionObservation{}, errors.New("mission observation requires an owned server and client manager")
	}
	var req debugMissionRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return debugMissionObservation{}, fmt.Errorf("missionRequest: %w", err)
	}
	if req.ClientID == "" || req.TimeoutMS > 120000 || operation != "client.mission_state" && req.TimeoutMS == 0 {
		return debugMissionObservation{}, errors.New("mission observation requires a client ID and waits require a timeout from 1 to 120000 milliseconds")
	}
	if req.TimeoutMS != 0 {
		waitContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
		defer cancel()
		ctx = waitContext
	}
	client, err := clientManager.State(req.ClientID)
	if err != nil {
		return debugMissionObservation{}, fmt.Errorf("missionClient: %w", err)
	}
	result := debugMissionObservation{ClientID: client.ClientID, InstanceID: client.InstanceID, PID: client.ProcessID}
	if !client.IsRunning || client.Status == "liveness_unavailable" || client.ProfileName == "" {
		return result, errors.New("mission observation requires a live owned client and profile")
	}
	initialSession := ""
	var initialUserID int64
	var initialGameID uint32
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, currentErr := clientManager.State(req.ClientID)
		if currentErr != nil {
			return result, fmt.Errorf("missionLiveness: %w", currentErr)
		}
		if current.InstanceID != client.InstanceID || current.ProcessID != client.ProcessID || current.ProfileName != client.ProfileName || !current.IsRunning || current.Status == "liveness_unavailable" {
			return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "missionIdentity", Message: "owned client changed or stopped during mission observation"}
		}
		profile, profileErr := host.ProfileState(ctx, client.ProfileName)
		if profileErr != nil {
			return result, fmt.Errorf("missionProfile: %w", profileErr)
		}
		result.State = profile
		contextErr := ctx.Err()
		if contextErr != nil {
			return result, debugMissionContext(operation, contextErr)
		}
		if operation == "client.mission_state" {
			return result, nil
		}
		if initialSession != "" && (!profile.IsAuthenticated || profile.SessionID != initialSession || profile.UserID != initialUserID) {
			return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "missionLogin", Message: "accepted login changed during mission wait"}
		}
		if initialGameID != 0 && profile.GameID != initialGameID {
			return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "missionGame", Message: "accepted game changed during mission wait"}
		}
		if profile.IsAuthenticated {
			initialSession = profile.SessionID
			initialUserID = profile.UserID
			if initialGameID == 0 && profile.GameID != 0 {
				initialGameID = profile.GameID
			}
		}
		if profile.IsAuthenticated && profile.GameID != 0 && profile.IsGameplayAvailable && profile.GameplayGameID == profile.GameID && profile.IsGameplayActive {
			if operation == "client.wait_deployment" && isDebugDeploymentReached(profile.SessionStage) {
				return result, nil
			}
			if operation == "client.wait_mission" && profile.SessionStage == "dungeon" && profile.IsDungeonSetupFinal {
				world, readyErr := waitDebugWorldReady(ctx, host, clientManager, payload)
				if readyErr != nil {
					return result, fmt.Errorf("missionWorldReady: %w", readyErr)
				}
				if world.GameID != profile.GameID {
					return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "missionReadyGame", Message: "game changed during world readiness wait"}
				}
				result.World = &world
				return result, nil
			}
		}
		select {
		case <-ctx.Done():
			return result, debugMissionContext(operation, ctx.Err())
		case <-ticker.C:
		}
	}
}

// This is a lifecycle threshold, not a requirement to catch one transient phase.
// Preparation and dungeon can already follow a different party member's Accept.
// It does not assert native UI readiness or observation of every earlier phase.
func isDebugDeploymentReached(stage string) bool {
	switch stage {
	case "chain-countdown-sent", "preparing", "dungeon":
		return true
	default:
		return false
	}
}

func debugMissionContext(operation string, err error) error {
	code := control.DeadlineExceeded
	if errors.Is(err, context.Canceled) {
		code = control.ErrorCode("cancelled")
	}
	message := "server dungeon commitment was not observed within the requested lifetime"
	contextTag := "missionWait"
	if operation == "client.wait_deployment" {
		message = "server deployment preparation was not observed within the requested lifetime"
		contextTag = "deploymentWait"
	}
	return &control.ScenarioError{Code: code, Context: contextTag, Message: message, Cause: err}
}
