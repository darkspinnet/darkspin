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

type controlArenaLobbyState struct {
	IsAvailable      bool    `json:"is_available"`
	IsLoaded         bool    `json:"is_loaded"`
	IsReady          bool    `json:"is_ready"`
	SquadIndex       *uint32 `json:"squad_index"`
	IsSquadValid     *bool   `json:"is_squad_valid"`
	IsAcceptDisabled *bool   `json:"is_accept_disabled"`
	IsAcceptVisible  *bool   `json:"is_accept_visible"`
}

// Accept dispatches the authored OnReadyClicked listener exactly once. Only
// pure observations are retried while waiting for the UI or server receipt.
func executeDebugArena(ctx context.Context, host *debugHost, clientManager *debugClientManager, operation string, payload json.RawMessage) (any, error) {
	if operation != "client.arena_lobby" && operation != "client.accept_arena" {
		return nil, errors.New("unsupported native arena operation")
	}
	var req debugMissionRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return nil, fmt.Errorf("arenaRequest: %w", err)
	}
	if operation == "client.arena_lobby" && req.TimeoutMS == 0 {
		req.TimeoutMS = 10000
	}
	if ctx == nil || host == nil || clientManager == nil || req.ClientID == "" || req.TimeoutMS == 0 || req.TimeoutMS > 120000 {
		return nil, errors.New("arena operation requires an owned client and timeout_ms from 1 to 120000")
	}
	waitContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	owner, err := observeDebugWorldOwner(waitContext, host, clientManager, req.ClientID)
	if err != nil {
		return nil, fmt.Errorf("arenaOwner: %w", err)
	}
	if operation == "client.arena_lobby" {
		result, inspectErr := clientManager.InspectArena(waitContext, req.ClientID)
		if inspectErr != nil {
			return result, fmt.Errorf("arenaSnapshot: %w", inspectErr)
		}
		err = recheckDebugWorldOwner(waitContext, host, clientManager, owner)
		if err != nil {
			return result, fmt.Errorf("arenaSnapshotOwner: %w", err)
		}
		return result, nil
	}
	if owner.profile.GameID == 0 {
		return nil, errors.New("arena acceptance requires an admitted game")
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var result any
	isDispatched := false
	for {
		err = recheckDebugWorldOwner(waitContext, host, clientManager, owner)
		if err != nil {
			return result, fmt.Errorf("arenaIdentity: %w", err)
		}
		profile, profileErr := host.ProfileState(waitContext, owner.client.ProfileName)
		if profileErr != nil {
			return result, fmt.Errorf("arenaReceipt: %w", profileErr)
		}
		if profile.IsGameplayActive && profile.GameplayGameID == owner.profile.GameID && !profile.IsArena {
			return result, errors.New("arena acceptance requires an Arena game")
		}
		if profile.IsArena && profile.GameplayGameID == owner.profile.GameID && profile.IsArenaSquadAccepted {
			err = recheckDebugWorldOwner(waitContext, host, clientManager, owner)
			if err != nil {
				return result, fmt.Errorf("arenaAcceptedOwner: %w", err)
			}
			return map[string]any{"is_already_accepted": !isDispatched, "action": result, "profile": profile}, nil
		}
		if !isDispatched {
			probe, probeErr := clientManager.InspectArena(waitContext, req.ClientID)
			result = probe
			if probeErr != nil {
				var failure *control.ScenarioError
				if !errors.As(probeErr, &failure) || failure.Code != control.NotAvailable {
					return result, fmt.Errorf("arenaProbe: %w", probeErr)
				}
			} else {
				receipt, isReceipt := probe.(map[string]any)
				if !isReceipt {
					return result, errors.New("arena snapshot receipt has an invalid shape")
				}
				lobby, isLobby := receipt["arena_lobby"].(*controlArenaLobbyState)
				if !isLobby || lobby == nil {
					return result, errors.New("arena snapshot receipt is missing native observations")
				}
				if profile.IsArena && profile.GameplayGameID == owner.profile.GameID && lobby.IsAvailable && lobby.IsLoaded && lobby.IsReady {
					err = recheckDebugWorldOwner(waitContext, host, clientManager, owner)
					if err != nil {
						return result, fmt.Errorf("arenaActionOwner: %w", err)
					}
					isDispatched = true
					result, err = clientManager.AcceptArena(waitContext, req.ClientID)
					if err != nil {
						return result, fmt.Errorf("arenaDispatch: %w", err)
					}
				}
			}
		}
		select {
		case <-waitContext.Done():
			return result, fmt.Errorf("arenaWait: %w", waitContext.Err())
		case <-ticker.C:
		}
	}
}
