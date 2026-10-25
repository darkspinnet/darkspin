//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Compose the guarded operations without replaying any native action. Partial
// step receipts are retained on failure; the caller must not blindly retry Enter.
func enterDebugMission(ctx context.Context, host *debugHost, clientManager *debugClientManager, payload json.RawMessage) (any, error) {
	var req debugMapRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return nil, fmt.Errorf("enterRequest: %w", err)
	}
	if ctx == nil || req.TimeoutMS == 0 || req.TimeoutMS > 120000 {
		return nil, errors.New("mission entry requires a context and timeout from 1 to 120000 milliseconds")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	selection, err := selectDebugMission(ctx, host, clientManager, payload)
	if err != nil {
		return selection, fmt.Errorf("enterSelect: %w", err)
	}
	actionPayload, err := json.Marshal(debugMissionRequest{ClientID: req.ClientID, TimeoutMS: req.TimeoutMS})
	if err != nil {
		return selection, fmt.Errorf("enterPayload: %w", err)
	}
	started, err := executeDebugMissionAction(ctx, host, clientManager, "client.start_mission", actionPayload)
	if err != nil {
		return started, fmt.Errorf("enterStart: %w", err)
	}
	continued, err := executeDebugMissionAction(ctx, host, clientManager, "client.continue_mission", actionPayload)
	if err != nil {
		return continued, fmt.Errorf("enterContinue: %w", err)
	}
	if continued.Observation.World != nil {
		return *continued.Observation.World, nil
	}
	world, err := waitDebugWorldReady(ctx, host, clientManager, actionPayload)
	if err != nil {
		return world, fmt.Errorf("enterReady: %w", err)
	}
	return world, nil
}
