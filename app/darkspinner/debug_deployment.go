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

type controlDeploymentState struct {
	IsAvailable             bool    `json:"is_available"`
	IsLoaded                bool    `json:"is_loaded"`
	IsReady                 bool    `json:"is_ready"`
	IsResultsScreen         bool    `json:"is_results_screen"`
	SquadIndex              *uint32 `json:"squad_index"`
	MajorDifficulty         *uint32 `json:"major_difficulty"`
	MinorDifficulty         *uint32 `json:"minor_difficulty"`
	IsSquadValid            *bool   `json:"is_squad_valid"`
	IsAcceptDisabled        *bool   `json:"is_accept_disabled"`
	IsAcceptVisible         *bool   `json:"is_accept_visible"`
	AreSquadButtonsDisabled *bool   `json:"are_squad_buttons_disabled"`
}

func observeDebugDeployment(ctx context.Context, clientManager *debugClientManager, operation string, payload json.RawMessage) (any, error) {
	if operation != "client.deployment_state" && operation != "client.wait_deployment_ready" {
		return nil, errors.New("unsupported deployment observation")
	}
	var req debugMissionRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return nil, fmt.Errorf("deploymentRequest: %w", err)
	}
	isWait := operation == "client.wait_deployment_ready"
	if req.ClientID == "" || req.TimeoutMS > 120000 || isWait && req.TimeoutMS == 0 {
		return nil, errors.New("deployment observation requires a client ID and waits require a timeout from 1 to 120000 milliseconds")
	}
	if req.TimeoutMS != 0 {
		waitContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
		defer cancel()
		ctx = waitContext
	}
	result, err := readDebugDeployment(ctx, clientManager, req.ClientID, isWait)
	if err != nil {
		return result, fmt.Errorf("deploymentObserve: %w", err)
	}
	return result, nil
}

func readDebugDeployment(ctx context.Context, clientManager *debugClientManager, clientID string, isWait bool) (any, error) {
	if clientManager == nil {
		return nil, errors.New("deployment observation requires an owned client manager")
	}
	client, err := clientManager.State(clientID)
	if err != nil {
		return nil, fmt.Errorf("deploymentClient: %w", err)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var result any
	for {
		contextErr := ctx.Err()
		if contextErr != nil {
			return result, debugDeploymentContext(contextErr)
		}
		current, stateErr := clientManager.State(clientID)
		if stateErr != nil {
			return result, fmt.Errorf("deploymentLiveness: %w", stateErr)
		}
		if current.InstanceID != client.InstanceID || current.ProcessID != client.ProcessID || current.ProfileName != client.ProfileName || !current.IsRunning || current.Status == "liveness_unavailable" {
			return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "deploymentIdentity", Message: "owned client changed or stopped during deployment observation"}
		}
		probe, probeErr := clientManager.InspectDeployment(ctx, clientID)
		if probeErr != nil {
			var failure *control.ScenarioError
			if !isWait || !errors.As(probeErr, &failure) || failure.Code != control.NotAvailable {
				return probe, fmt.Errorf("deploymentProbe: %w", probeErr)
			}
			// Only explicit unavailability of this pure query can be retried.
			// Unknown outcomes and native action invocations are never replayed.
		} else {
			result = probe
			if !isWait {
				return result, nil
			}
			receipt, isReceipt := probe.(map[string]any)
			if !isReceipt {
				return result, errors.New("deployment receipt has an invalid shape")
			}
			deployment, isDeployment := receipt["deployment_state"].(*controlDeploymentState)
			if !isDeployment || deployment == nil {
				return result, errors.New("deployment receipt is missing native observations")
			}
			if deployment.IsAvailable && deployment.IsLoaded && deployment.IsReady {
				return result, nil
			}
		}
		select {
		case <-ctx.Done():
			return result, debugDeploymentContext(ctx.Err())
		case <-ticker.C:
		}
	}
}

func debugDeploymentContext(err error) error {
	code := control.DeadlineExceeded
	if errors.Is(err, context.Canceled) {
		code = control.ErrorCode("cancelled")
	}
	return &control.ScenarioError{Code: code, Context: "deploymentUIWait", Message: "eligible native deployment UI was not observed", Cause: err}
}
