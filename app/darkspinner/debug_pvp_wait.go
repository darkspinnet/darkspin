//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// A committed dungeon can still be in the arena opening countdown. Observe
// the actual match producer before reporting that PvP is ready for combat.
func waitDebugPVP(ctx context.Context, host *debugHost, clientManager *debugClientManager, payload json.RawMessage) (debugWorldState, error) {
	var result debugWorldState
	var req debugMissionRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return result, fmt.Errorf("pvpWaitRequest: %w", err)
	}
	if ctx == nil || req.ClientID == "" || req.TimeoutMS == 0 || req.TimeoutMS > 120000 {
		return result, errors.New("PvP wait requires an owned client and timeout from 1 to 120000 milliseconds")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	owner, err := observeDebugWorldOwner(ctx, host, clientManager, req.ClientID)
	if err != nil {
		return result, fmt.Errorf("pvpWaitOwner: %w", err)
	}
	if owner.profile.GameID == 0 {
		return result, errors.New("PvP wait requires an already formed arena")
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		err = recheckDebugWorldOwner(ctx, host, clientManager, owner)
		if err != nil {
			return result, fmt.Errorf("pvpWaitIdentity: %w", err)
		}
		profile, profileErr := host.ProfileState(ctx, owner.client.ProfileName)
		if profileErr != nil {
			return result, fmt.Errorf("pvpWaitProfile: %w", profileErr)
		}
		if profile.IsGameplayAvailable && profile.IsGameplayActive && profile.GameplayGameID == owner.profile.GameID {
			if !profile.IsArena {
				return result, errors.New("PvP wait observed a non-arena game")
			}
			if profile.IsArenaSquadAccepted && profile.IsArenaCombatActive {
				result, err = waitDebugWorldReady(ctx, host, clientManager, payload)
				if err != nil {
					return result, fmt.Errorf("pvpWaitWorld: %w", err)
				}
				err = recheckDebugWorldOwner(ctx, host, clientManager, owner)
				if err != nil {
					return result, fmt.Errorf("pvpWaitFinal: %w", err)
				}
				return result, nil
			}
		}
		select {
		case <-ctx.Done():
			return result, fmt.Errorf("pvpWaitDeadline: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
