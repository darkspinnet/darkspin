//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/scenario/control"
)

type debugWorldSpawnRequest struct {
	ClientID  string `json:"client_id"`
	Noun      string `json:"noun,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	TimeoutMS uint32 `json:"timeout_ms,omitempty"`
}

// Publication is the ordinary response-send commitment, never native pixels.
type debugWorldSpawn struct {
	ClientID            string `json:"client_id"`
	InstanceID          string `json:"instance_id"`
	PID                 uint32 `json:"pid"`
	UserID              int64  `json:"user_id"`
	RequestID           string `json:"request_id"`
	State               string `json:"state"`
	GameID              uint32 `json:"game_id"`
	PeerGeneration      uint64 `json:"peer_generation"`
	TransportGeneration uint64 `json:"transport_generation"`
	ZoneGeneration      uint64 `json:"zone_generation"`
	ObjectID            uint32 `json:"object_id"`
	Noun                string `json:"noun"`
	IsAdmitted          bool   `json:"is_admitted"`
	IsAIEnabled         bool   `json:"is_ai_enabled"`
	IsPublished         bool   `json:"is_published"`
	IsNativeObserved    bool   `json:"is_native_observed"`
	Failure             string `json:"failure,omitempty"`
	Source              string `json:"source"`
}

func debugWorldSpawnDTO(owner debugWorldOwner, receipt scenario.SpawnReceipt) debugWorldSpawn {
	return debugWorldSpawn{ClientID: owner.client.ClientID, InstanceID: owner.client.InstanceID, PID: owner.client.ProcessID, UserID: owner.client.UserID,
		RequestID: receipt.RequestID, State: receipt.State, GameID: receipt.GameID, PeerGeneration: receipt.PeerGeneration, TransportGeneration: receipt.TransportGeneration, ZoneGeneration: receipt.ZoneGeneration, IsAIEnabled: receipt.IsAIEnabled,
		ObjectID: receipt.ObjectID, Noun: receipt.NounName, IsAdmitted: receipt.IsAdmitted, IsPublished: receipt.IsPublished, IsNativeObserved: false, Failure: receipt.Failure, Source: "server_spawn_receipt"}
}

func inspectDebugWorldSpawn(ctx context.Context, host *debugHost, owner debugWorldOwner, receipt scenario.SpawnReceipt) error {
	if receipt.RunID != host.runID || receipt.UserID != owner.client.UserID || receipt.GameID != owner.profile.GameID {
		return &control.ScenarioError{Code: control.OwnershipLost, Context: "spawnReceipt", Message: "spawn receipt does not belong to this owned run/account/game"}
	}
	world, err := host.server.DebugWorldState(ctx, owner.client.UserID, owner.profile.GameID)
	if err != nil {
		return fmt.Errorf("spawnWorld: %w", err)
	}
	if !world.IsAvailable {
		return &control.ScenarioError{Code: control.NotAvailable, Context: "spawnWorld", Message: world.UnavailableReason}
	}
	if world.GameID != receipt.GameID || world.PeerGeneration != receipt.PeerGeneration || world.TransportGeneration != receipt.TransportGeneration || world.ZoneGeneration != receipt.ZoneGeneration {
		return &control.ScenarioError{Code: control.OwnershipLost, Context: "spawnEpoch", Message: "spawn receipt belongs to an earlier gameplay peer or zone"}
	}
	return nil
}

func executeDebugWorldSpawn(ctx context.Context, host *debugHost, clientManager *debugClientManager, operation string, payload json.RawMessage) (debugWorldSpawn, error) {
	result := debugWorldSpawn{Source: "server_spawn_receipt"}
	var req debugWorldSpawnRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return result, fmt.Errorf("spawnPayload: %w", err)
	}
	if req.TimeoutMS > 120000 {
		return result, errors.New("spawn timeout must not exceed 120000 milliseconds")
	}
	if operation == "client.spawn_state" {
		if req.RequestID == "" || req.Noun != "" || req.TimeoutMS != 0 {
			return result, errors.New("spawn state requires only an owned client ID and request ID")
		}
	} else if req.Noun == "" || req.RequestID != "" {
		return result, errors.New("spawn requires an exact noun filename and no request ID")
	}
	if req.TimeoutMS == 0 {
		req.TimeoutMS = 10000
	}
	waitContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	owner, err := observeDebugWorldOwner(waitContext, host, clientManager, req.ClientID)
	if err != nil {
		return result, fmt.Errorf("spawnOwner: %w", err)
	}
	if owner.profile.GameID == 0 || !owner.profile.IsGameplayActive {
		return result, errors.New("spawn requires an admitted live dungeon")
	}
	var receipt scenario.SpawnReceipt
	if operation == "client.spawn_state" {
		receipt, err = host.server.DebugSpawnState(waitContext, host.runID, owner.client.UserID, req.RequestID)
		if err != nil {
			return result, fmt.Errorf("spawnRead: %w", err)
		}
	} else {
		world, worldErr := host.server.DebugWorldState(waitContext, owner.client.UserID, owner.profile.GameID)
		if worldErr != nil {
			return result, fmt.Errorf("spawnBefore: %w", worldErr)
		}
		if !world.IsAvailable || world.Hero == nil {
			return result, &control.ScenarioError{Code: control.NotAvailable, Context: "spawnWorld", Message: "spawn requires an available committed world and deployed hero"}
		}
		err = recheckDebugWorldOwner(waitContext, host, clientManager, owner)
		if err != nil {
			return result, fmt.Errorf("spawnPrecheck: %w", err)
		}
		spawnRequest := scenario.SpawnRequest{RunID: host.runID, UserID: owner.client.UserID, GameID: world.GameID, PeerGeneration: world.PeerGeneration, ZoneGeneration: world.ZoneGeneration, NounName: req.Noun}
		if operation == "client.spawn_controlled_npc" {
			isAIEnabled := false
			spawnRequest.IsAIEnabled = &isAIEnabled
		}
		receipt, err = host.server.DebugSpawnNPC(waitContext, spawnRequest)
		if err != nil {
			return debugWorldSpawnDTO(owner, receipt), fmt.Errorf("spawnQueue: %w", err)
		}
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		result = debugWorldSpawnDTO(owner, receipt)
		err = recheckDebugWorldOwner(waitContext, host, clientManager, owner)
		if err != nil {
			return result, fmt.Errorf("spawnIdentity: %w", err)
		}
		err = inspectDebugWorldSpawn(waitContext, host, owner, receipt)
		if err != nil {
			return result, fmt.Errorf("spawnInspect: %w", err)
		}
		if receipt.State == "failed" || receipt.Failure != "" {
			return result, &control.ScenarioError{Code: control.NotAvailable, Context: "spawnFailed", Message: receipt.Failure}
		}
		if (operation != "client.spawn_npc" && operation != "client.spawn_controlled_npc") || receipt.IsPublished {
			return result, nil
		}
		select {
		case <-waitContext.Done():
			return result, &control.ScenarioError{Code: control.DeadlineExceeded, Context: "spawnPublication", Message: "spawn publication was not observed before the deadline; the queued command is not replayed or cancelled", Cause: waitContext.Err()}
		case <-ticker.C:
		}
		receipt, err = host.server.DebugSpawnState(waitContext, host.runID, owner.client.UserID, receipt.RequestID)
		if err != nil {
			return result, fmt.Errorf("spawnPoll: %w", err)
		}
	}
}
