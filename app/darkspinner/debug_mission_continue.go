//go:build scenario

package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario/control"
)

// A peer's Accept can advance this member through preparing into dungeon before
// its own UI becomes eligible. Poll server completion alongside the pure native
// guard; do not require the earlier countdown stage to remain visible.
func waitDebugMissionContinue(ctx context.Context, host *debugHost, clientManager *debugClientManager, actor *debugMissionActionActor, result *debugMissionActionResult) (bool, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := sampleDebugMissionActor(ctx, host, clientManager, actor.client.ClientID)
		if err != nil {
			return false, fmt.Errorf("continueProfile: %w", err)
		}
		result.Observation = debugMissionObservation{ClientID: current.client.ClientID, InstanceID: current.client.InstanceID, PID: current.client.ProcessID, State: current.profile}
		if !isDebugMissionActionActor(*actor, current) {
			return false, debugMissionActionOwnership("owned process or accepted login changed while waiting to Continue")
		}
		if actor.profile.GameID == 0 && current.profile.GameID != 0 {
			actor.profile.GameID = current.profile.GameID
		}
		if current.profile.GameID != actor.profile.GameID {
			return false, debugMissionActionOwnership("mission game changed while waiting to Continue")
		}
		if isDebugMissionCommitted(current.profile, actor.profile.GameID) {
			return true, nil
		}
		if isDebugMissionActionObservation(*actor, result.Observation, actor.profile.GameID) {
			probe, probeErr := clientManager.InspectDeployment(ctx, actor.client.ClientID)
			if probe != nil {
				result.Readiness = probe
			}
			if probeErr != nil {
				var failure *control.ScenarioError
				if !errors.As(probeErr, &failure) || failure.Code != control.NotAvailable {
					return false, fmt.Errorf("continueProbe: %w", probeErr)
				}
				// Only a pure query's explicit unavailable result is polled again.
				// Mutations and unknown command outcomes are never retried.
			} else {
				receipt, isReceipt := probe.(map[string]any)
				if !isReceipt {
					return false, errors.New("Continue readiness receipt has an invalid shape")
				}
				deployment, isDeployment := receipt["deployment_state"].(*controlDeploymentState)
				if !isDeployment || deployment == nil {
					return false, errors.New("Continue readiness receipt is missing native observations")
				}
				if deployment.IsAvailable && deployment.IsLoaded && deployment.IsReady {
					return false, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("continueWait: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func isDebugMissionCommitted(profile debugProfileState, gameID uint32) bool {
	return profile.IsAuthenticated && gameID != 0 && profile.GameID == gameID && profile.IsGameplayAvailable && profile.GameplayGameID == gameID && profile.IsGameplayActive && profile.SessionStage == "dungeon" && profile.IsDungeonSetupFinal
}

func completeDebugMissionEntry(ctx context.Context, host *debugHost, clientManager *debugClientManager, actor debugMissionActionActor, result *debugMissionActionResult) error {
	current, err := sampleDebugMissionActor(ctx, host, clientManager, actor.client.ClientID)
	if err != nil {
		return fmt.Errorf("continueEntryProfile: %w", err)
	}
	result.Observation = debugMissionObservation{ClientID: current.client.ClientID, InstanceID: current.client.InstanceID, PID: current.client.ProcessID, State: current.profile}
	if !isDebugMissionActionActor(actor, current) || !isDebugMissionCommitted(current.profile, actor.profile.GameID) {
		return debugMissionActionOwnership("owned process, accepted login, or committed mission changed before Continue completion")
	}
	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("continueEntryContext: %w", err)
	}
	result.IsAlreadyEntered = true
	return nil
}

// A negative authored-invocation receipt is conclusive only for explicit native
// refusal. Disconnects, expired receipts, and unknown outcomes remain failures.
func isDebugContinueRefused(action any, err error, actor debugMissionActionActor) bool {
	var failure *control.ScenarioError
	if !errors.As(err, &failure) || (failure.Code != control.NotAvailable && failure.Code != control.ErrorCode("native_rejected")) {
		return false
	}
	receipt, isReceipt := action.(map[string]any)
	if !isReceipt {
		return false
	}
	isAttempted, isAttemptedKnown := receipt["is_invocation_attempted"].(bool)
	isAccepted, isAcceptedKnown := receipt["is_invocation_accepted"].(bool)
	if !isAttemptedKnown || isAttempted || !isAcceptedKnown || isAccepted {
		return false
	}
	status, isStatus := receipt["status"].(string)
	if !isStatus || (status != "unavailable" && status != "rejected") {
		return false
	}
	return receipt["client_id"] == actor.client.ClientID && receipt["instance_id"] == actor.client.InstanceID && receipt["pid"] == actor.client.ProcessID
}

// After an explicit no-invocation refusal, only observe the pinned game. Never
// retry the Continue command even if its button becomes eligible again.
func waitDebugMissionCommit(ctx context.Context, host *debugHost, clientManager *debugClientManager, actor debugMissionActionActor, result *debugMissionActionResult) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	isFirstSample := true
	for {
		current, err := sampleDebugMissionActor(ctx, host, clientManager, actor.client.ClientID)
		if err != nil {
			return fmt.Errorf("continueRefusalProfile: %w", err)
		}
		result.Observation = debugMissionObservation{ClientID: current.client.ClientID, InstanceID: current.client.InstanceID, PID: current.client.ProcessID, State: current.profile}
		if !isDebugMissionActionActor(actor, current) || current.profile.GameID != actor.profile.GameID {
			return debugMissionActionOwnership("owned process, accepted login, or mission changed after native Continue refusal")
		}
		if isFirstSample {
			if !isDebugMissionActionObservation(actor, result.Observation, actor.profile.GameID) {
				return errors.New("Continue refusal has no matching active gameplay progression evidence")
			}
			switch current.profile.SessionStage {
			case "preparing", "resume-pending", "dungeon":
			default:
				return errors.New("Continue refusal occurred before shared mission progression; the native guard failure remains authoritative")
			}
			isFirstSample = false
		}
		if isDebugMissionCommitted(current.profile, actor.profile.GameID) {
			err = completeDebugMissionEntry(ctx, host, clientManager, actor, result)
			if err != nil {
				return fmt.Errorf("continueRefusalFinal: %w", err)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("continueRefusalWait: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
