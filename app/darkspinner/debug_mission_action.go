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

type debugMissionActionResult struct {
	IsAlreadyEntered bool                    `json:"is_already_entered"`
	Readiness        any                     `json:"readiness,omitempty"`
	Action           any                     `json:"action"`
	Observation      debugMissionObservation `json:"observation"`
}

type debugMissionActionActor struct {
	client  debugClientState
	profile debugProfileState
}

// Each invocation dispatches at most one native action. Observation failure,
// cancellation, or unknown processing outcome must never replay that action.
func executeDebugMissionAction(ctx context.Context, host *debugHost, clientManager *debugClientManager, operation string, payload json.RawMessage) (debugMissionActionResult, error) {
	result := debugMissionActionResult{}
	if operation != "client.start_mission" && operation != "client.continue_mission" {
		return result, errors.New("unsupported mission action")
	}
	if ctx == nil || host == nil || clientManager == nil {
		return result, errors.New("mission action requires context and an owned host and client manager")
	}
	var req debugMissionRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return result, fmt.Errorf("missionActionDecode: %w", err)
	}
	if req.ClientID == "" || req.TimeoutMS == 0 || req.TimeoutMS > 120000 {
		return result, errors.New("mission action requires client_id and timeout_ms from 1 to 120000")
	}
	actionContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	actor, err := sampleDebugMissionActor(actionContext, host, clientManager, req.ClientID)
	if err != nil {
		return result, fmt.Errorf("missionActionActor: %w", err)
	}
	result.Observation = debugMissionObservation{ClientID: actor.client.ClientID, InstanceID: actor.client.InstanceID, PID: actor.client.ProcessID, State: actor.profile}
	if operation == "client.start_mission" {
		if actor.profile.GameID != 0 {
			return result, errors.New("mission start requires an authenticated client outside an active game; do not replay Start")
		}
		selectionPayload, marshalErr := json.Marshal(debugMapRequest{ClientID: req.ClientID, TimeoutMS: req.TimeoutMS})
		if marshalErr != nil {
			return result, fmt.Errorf("missionSelectionPayload: %w", marshalErr)
		}
		selection, selectionErr := observeDebugMap(actionContext, clientManager, "client.wait_selection", selectionPayload)
		result.Readiness = selection
		if selectionErr != nil {
			return result, fmt.Errorf("missionSelectionWait: %w", selectionErr)
		}
		if selection == nil {
			return result, errors.New("mission selection wait returned no observation")
		}
	} else {
		isAlreadyEntered, readinessErr := waitDebugMissionContinue(actionContext, host, clientManager, &actor, &result)
		if readinessErr != nil {
			return result, fmt.Errorf("missionContinueReady: %w", readinessErr)
		}
		if isAlreadyEntered {
			err = completeDebugMissionEntry(actionContext, host, clientManager, actor, &result)
			if err != nil {
				return result, fmt.Errorf("missionEntryFinal: %w", err)
			}
			return result, nil
		}
	}
	before, err := sampleDebugMissionActor(actionContext, host, clientManager, req.ClientID)
	if err != nil {
		return result, fmt.Errorf("missionActionRecheck: %w", err)
	}
	result.Observation.State = before.profile
	if !isDebugMissionActionActor(actor, before) || before.profile.GameID != actor.profile.GameID {
		return result, debugMissionActionOwnership("owned client, accepted login, or game changed before mission action")
	}
	if operation == "client.continue_mission" && isDebugMissionCommitted(before.profile, actor.profile.GameID) {
		err = completeDebugMissionEntry(actionContext, host, clientManager, actor, &result)
		if err != nil {
			return result, fmt.Errorf("missionEntryRecheck: %w", err)
		}
		return result, nil
	}
	err = actionContext.Err()
	if err != nil {
		return result, fmt.Errorf("missionActionContext: %w", err)
	}
	if operation == "client.start_mission" {
		result.Action, err = clientManager.StartMission(actionContext, req.ClientID)
	} else {
		result.Action, err = clientManager.ContinueMission(actionContext, req.ClientID)
	}
	if err != nil {
		if operation == "client.continue_mission" && isDebugContinueRefused(result.Action, err, actor) {
			completionErr := waitDebugMissionCommit(actionContext, host, clientManager, actor, &result)
			if completionErr == nil {
				return result, nil
			}
			return result, fmt.Errorf("missionContinueRefusal: %w", errors.Join(err, completionErr))
		}
		return result, fmt.Errorf("missionNativeAction: %w", err)
	}
	if result.Action == nil {
		return result, errors.New("native mission action returned no receipt; do not replay the action")
	}
	waitOperation := "client.wait_mission"
	if operation == "client.start_mission" {
		waitOperation = "client.wait_deployment"
	}
	observation, err := observeDebugMission(actionContext, host, clientManager, waitOperation, payload)
	result.Observation = observation
	if err != nil {
		return result, fmt.Errorf("missionActionWait: %w", err)
	}
	expectedGameID := actor.profile.GameID
	if operation == "client.start_mission" {
		expectedGameID = observation.State.GameID
	}
	if expectedGameID == 0 || !isDebugMissionActionObservation(actor, observation, expectedGameID) {
		return result, debugMissionActionOwnership("owned client, accepted login, or mission changed after action")
	}
	after, err := sampleDebugMissionActor(actionContext, host, clientManager, req.ClientID)
	if err != nil {
		return result, fmt.Errorf("missionActionFinal: %w", err)
	}
	if !isDebugMissionActionActor(actor, after) || after.profile.GameID != expectedGameID || !after.profile.IsGameplayAvailable || after.profile.GameplayGameID != expectedGameID {
		return result, debugMissionActionOwnership("owned client, accepted login, or mission changed after observation")
	}
	return result, nil
}

func sampleDebugMissionActor(ctx context.Context, host *debugHost, clientManager *debugClientManager, clientID string) (debugMissionActionActor, error) {
	result := debugMissionActionActor{}
	client, err := clientManager.State(clientID)
	if err != nil {
		return result, fmt.Errorf("missionActorClient: %w", err)
	}
	result.client = client
	if !client.IsRunning || client.Status == "liveness_unavailable" || client.InstanceID == "" || client.ProcessID == 0 || client.ProfileName == "" {
		return result, errors.New("mission action requires a live owned process and profile")
	}
	profile, err := host.ProfileState(ctx, client.ProfileName)
	if err != nil {
		return result, fmt.Errorf("missionActorProfile: %w", err)
	}
	result.profile = profile
	if !profile.IsAuthenticated || profile.SessionID == "" {
		return result, errors.New("mission action requires an accepted authenticated session")
	}
	current, err := clientManager.State(clientID)
	if err != nil {
		return result, fmt.Errorf("missionActorLive: %w", err)
	}
	if !current.IsRunning || current.Status == "liveness_unavailable" || current.InstanceID != client.InstanceID || current.ProcessID != client.ProcessID || current.ProfileName != client.ProfileName {
		return result, debugMissionActionOwnership("owned client changed while sampling mission action identity")
	}
	return result, nil
}

func isDebugMissionActionActor(before, after debugMissionActionActor) bool {
	return before.client.ClientID == after.client.ClientID && before.client.InstanceID == after.client.InstanceID && before.client.ProcessID == after.client.ProcessID && before.client.ProfileName == after.client.ProfileName && after.profile.IsAuthenticated && before.profile.SessionID == after.profile.SessionID && before.profile.UserID == after.profile.UserID
}

func isDebugMissionActionObservation(actor debugMissionActionActor, observation debugMissionObservation, gameID uint32) bool {
	return observation.ClientID == actor.client.ClientID && observation.InstanceID == actor.client.InstanceID && observation.PID == actor.client.ProcessID && observation.State.IsAuthenticated && observation.State.SessionID == actor.profile.SessionID && observation.State.UserID == actor.profile.UserID && gameID != 0 && observation.State.GameID == gameID && observation.State.IsGameplayAvailable && observation.State.GameplayGameID == gameID && observation.State.IsGameplayActive
}

func debugMissionActionOwnership(message string) error {
	return &control.ScenarioError{Code: control.OwnershipLost, Context: "missionActionIdentity", Message: message}
}
