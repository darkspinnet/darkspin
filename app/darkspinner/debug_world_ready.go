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

func observeDebugLivingWorld(ctx context.Context, host *debugHost, clientManager *debugClientManager, payload json.RawMessage) (debugWorldState, error) {
	result, err := observeDebugWorld(ctx, host, clientManager, payload)
	if err != nil {
		return result, fmt.Errorf("livingObserve: %w", err)
	}
	if !result.IsAvailable || result.IsZoneTerminal || result.Hero == nil || result.Hero.IsDefeated || result.Hero.HitPoint <= 0 {
		return result, errors.New("world has no available living deployed hero")
	}
	return result, nil
}

// Require committed server setup and the native active world/controlled actor.
// This does not attest visibility of every object. Only explicit unavailability
// is polled; failed or unknown commands are never replayed.
func waitDebugWorldReady(ctx context.Context, host *debugHost, clientManager *debugClientManager, payload json.RawMessage) (debugWorldState, error) {
	var result debugWorldState
	var req debugMissionRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return result, fmt.Errorf("readyRequest: %w", err)
	}
	if req.ClientID == "" || req.TimeoutMS == 0 || req.TimeoutMS > 120000 {
		return result, errors.New("world readiness requires a client ID and timeout from 1 to 120000 milliseconds")
	}
	if ctx == nil {
		return result, errors.New("world readiness requires a context")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	owner, err := observeDebugWorldOwner(ctx, host, clientManager, req.ClientID)
	if err != nil {
		return result, fmt.Errorf("readyOwner: %w", err)
	}
	query, err := json.Marshal(map[string]string{"client_id": req.ClientID})
	if err != nil {
		return result, fmt.Errorf("readyQuery: %w", err)
	}
	gameID := owner.profile.GameID
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, ownerErr := observeDebugWorldOwner(ctx, host, clientManager, req.ClientID)
		if ownerErr != nil {
			return result, fmt.Errorf("readyIdentity: %w", ownerErr)
		}
		if current.client.InstanceID != owner.client.InstanceID || current.client.ProcessID != owner.client.ProcessID ||
			current.client.ProfileName != owner.client.ProfileName || current.profile.UserID != owner.profile.UserID ||
			current.profile.SessionID != owner.profile.SessionID || gameID != 0 && current.profile.GameID != gameID {
			return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "worldReady", Message: "owned process, login or admitted game changed while waiting for world readiness"}
		}
		if gameID == 0 {
			gameID = current.profile.GameID
		}
		result, err = observeDebugWorld(ctx, host, clientManager, query)
		if err != nil {
			return result, fmt.Errorf("readyObserve: %w", err)
		}
		if gameID != 0 && result.GameID != gameID {
			return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "worldReadyGame", Message: "admitted game changed during world observation"}
		}
		if result.IsAvailable && result.Hero != nil && result.Hero.ObjectID != 0 && !result.IsZoneTerminal {
			probe, native, nativeErr := observeDebugHeroNative(ctx, clientManager, current)
			if nativeErr != nil {
				var failure *control.ScenarioError
				if !errors.As(nativeErr, &failure) || failure.Code != control.NotAvailable {
					return result, fmt.Errorf("readyNative: %w", nativeErr)
				}
			} else {
				if probe == nil || native == nil {
					return result, errors.New("world readiness omitted native evidence")
				}
				err = recheckDebugWorldOwner(ctx, host, clientManager, current)
				if err != nil {
					return result, fmt.Errorf("readyRecheck: %w", err)
				}
				after, observeErr := observeDebugWorld(ctx, host, clientManager, query)
				if observeErr != nil {
					return result, fmt.Errorf("readyFinal: %w", observeErr)
				}
				if after.GameID != result.GameID || after.PeerGeneration != result.PeerGeneration || after.TransportGeneration != result.TransportGeneration || after.ZoneGeneration != result.ZoneGeneration {
					return after, &control.ScenarioError{Code: control.OwnershipLost, Context: "worldReadyEpoch", Message: "world generation changed during native readiness observation"}
				}
				if after.IsAvailable && after.Hero != nil && after.Hero.ObjectID == result.Hero.ObjectID && !after.IsZoneTerminal {
					after.NativeReadiness = native
					return after, nil
				}
			}
		}
		if result.IsZoneTerminal {
			return result, errors.New("world became terminal before readiness")
		}
		select {
		case <-ctx.Done():
			code := control.DeadlineExceeded
			if errors.Is(ctx.Err(), context.Canceled) {
				code = control.ErrorCode("cancelled")
			}
			return result, &control.ScenarioError{Code: code, Context: "worldReady", Message: "server world readiness was not observed", Cause: ctx.Err()}
		case <-ticker.C:
		}
	}
}
