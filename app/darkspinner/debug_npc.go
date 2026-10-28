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

type debugNPCIdentity struct {
	UserID              int64  `json:"user_id"`
	GameID              uint32 `json:"game_id"`
	PeerGeneration      uint64 `json:"peer_generation"`
	TransportGeneration uint64 `json:"transport_generation"`
	ZoneGeneration      uint64 `json:"zone_generation"`
	SpawnRequestID      string `json:"spawn_request_id"`
	ObjectID            uint32 `json:"object_id"`
}

type debugNPCCommand struct {
	Identity                        debugNPCIdentity    `json:"identity"`
	RequestID                       string              `json:"request_id"`
	Operation                       string              `json:"operation"`
	State                           string              `json:"state"`
	IsAccepted                      bool                `json:"is_accepted"`
	IsCompleted                     bool                `json:"is_completed"`
	IsSettled                       bool                `json:"is_settled"`
	IsResponseCommitted             bool                `json:"is_response_committed"`
	ActionGeneration                uint64              `json:"action_generation"`
	Failure                         string              `json:"failure,omitempty"`
	SupersededRequestID             string              `json:"superseded_request_id,omitempty"`
	SupersededFailure               string              `json:"superseded_failure,omitempty"`
	RequestedDestination            *debugWorldPosition `json:"requested_destination,omitempty"`
	ProjectedDestination            *debugWorldPosition `json:"projected_destination,omitempty"`
	IsProjectedDestinationAvailable bool                `json:"is_projected_destination_available"`
}

type debugNPCState struct {
	ClientID              string             `json:"client_id"`
	InstanceID            string             `json:"instance_id"`
	PID                   uint32             `json:"pid"`
	Identity              debugNPCIdentity   `json:"identity"`
	IsAvailable           bool               `json:"is_available"`
	UnavailableReason     string             `json:"unavailable_reason,omitempty"`
	IsAIEnabled           bool               `json:"is_ai_enabled"`
	Position              debugWorldPosition `json:"position"`
	HitPoint              float32            `json:"hit_point"`
	IsDead                bool               `json:"is_dead"`
	SupportedAbilityNames []string           `json:"supported_ability_names"`
	ActionGeneration      uint64             `json:"action_generation"`
	LastCommand           *debugNPCCommand   `json:"last_command,omitempty"`
}

type debugNPCRequest struct {
	ClientID            string                 `json:"client_id"`
	InstanceID          string                 `json:"instance_id"`
	PID                 uint32                 `json:"pid"`
	UserID              int64                  `json:"user_id"`
	GameID              uint32                 `json:"game_id"`
	PeerGeneration      uint64                 `json:"peer_generation"`
	TransportGeneration uint64                 `json:"transport_generation"`
	ZoneGeneration      uint64                 `json:"zone_generation"`
	SpawnRequestID      string                 `json:"spawn_request_id"`
	ObjectID            uint32                 `json:"object_id"`
	IsAIEnabled         *bool                  `json:"is_ai_enabled,omitempty"`
	Destination         *debugPathPointRequest `json:"destination,omitempty"`
	AbilityName         string                 `json:"ability_name,omitempty"`
	TargetObjectID      uint32                 `json:"target_object_id,omitempty"`
	TimeoutMS           uint32                 `json:"timeout_ms,omitempty"`
}

func debugNPCIdentityDTO(identity scenario.ControlledNPCIdentity) debugNPCIdentity {
	return debugNPCIdentity{UserID: identity.UserID, GameID: identity.GameID, PeerGeneration: identity.PeerGeneration,
		TransportGeneration: identity.TransportGeneration, ZoneGeneration: identity.ZoneGeneration,
		SpawnRequestID: identity.SpawnRequestID, ObjectID: identity.ObjectID}
}

func debugNPCCommandDTO(receipt scenario.NPCControlReceipt) debugNPCCommand {
	result := debugNPCCommand{Identity: debugNPCIdentityDTO(receipt.Identity), RequestID: receipt.RequestID, Operation: receipt.Operation,
		State: receipt.State, IsAccepted: receipt.IsAccepted, IsCompleted: receipt.IsCompleted, IsSettled: receipt.IsSettled,
		IsResponseCommitted: receipt.IsResponseCommitted, ActionGeneration: receipt.ActionGeneration, Failure: receipt.Failure,
		SupersededRequestID: receipt.SupersededRequestID, SupersededFailure: receipt.SupersededFailure,
		IsProjectedDestinationAvailable: receipt.ProjectedDestination != nil}
	if receipt.RequestedDestination != nil {
		destination := debugWorldPositionDTO(*receipt.RequestedDestination)
		result.RequestedDestination = &destination
	}
	if receipt.ProjectedDestination != nil {
		destination := debugWorldPositionDTO(*receipt.ProjectedDestination)
		result.ProjectedDestination = &destination
	}
	return result
}

func inspectDebugNPCOwner(ctx context.Context, host *debugHost, manager *debugClientManager, owner debugWorldOwner, identity scenario.ControlledNPCIdentity) error {
	err := recheckDebugWorldOwner(ctx, host, manager, owner)
	if err != nil {
		return fmt.Errorf("npcOwner: %w", err)
	}
	world, err := host.server.DebugWorldState(ctx, identity.UserID, identity.GameID)
	if err != nil {
		return fmt.Errorf("npcWorld: %w", err)
	}
	if !world.IsAvailable {
		return &control.ScenarioError{Code: control.NotAvailable, Context: "npcWorld", Message: world.UnavailableReason}
	}
	if world.GameID != identity.GameID || world.PeerGeneration != identity.PeerGeneration || world.TransportGeneration != identity.TransportGeneration || world.ZoneGeneration != identity.ZoneGeneration {
		return &control.ScenarioError{Code: control.OwnershipLost, Context: "npcEpoch", Message: "controlled NPC belongs to an earlier game, peer, transport or zone"}
	}
	return nil
}

func executeDebugNPC(ctx context.Context, host *debugHost, manager *debugClientManager, operation string, payload json.RawMessage) (any, error) {
	var req debugNPCRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return nil, fmt.Errorf("npcPayload: %w", err)
	}
	if req.InstanceID == "" || req.PID == 0 || req.UserID <= 0 || req.GameID == 0 || req.PeerGeneration == 0 || req.TransportGeneration == 0 || req.ZoneGeneration == 0 || req.SpawnRequestID == "" || req.ObjectID == 0 {
		return nil, errors.New("controlled NPC requires a complete captured owned spawn identity")
	}
	if req.TimeoutMS > 120000 {
		return nil, errors.New("NPC timeout must not exceed 120000 milliseconds")
	}
	if req.TimeoutMS == 0 {
		req.TimeoutMS = 60000
	}
	waitContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	owner, err := observeDebugWorldOwner(waitContext, host, manager, req.ClientID)
	if err != nil {
		return nil, fmt.Errorf("npcClient: %w", err)
	}
	if owner.client.InstanceID != req.InstanceID || owner.client.ProcessID != req.PID || owner.client.UserID != req.UserID || owner.profile.GameID != req.GameID {
		return nil, &control.ScenarioError{Code: control.OwnershipLost, Context: "npcLaunch", Message: "controlled NPC handle no longer belongs to this owned client session"}
	}
	identity := scenario.ControlledNPCIdentity{RunID: host.runID, UserID: req.UserID, GameID: req.GameID, PeerGeneration: req.PeerGeneration,
		TransportGeneration: req.TransportGeneration, ZoneGeneration: req.ZoneGeneration, SpawnRequestID: req.SpawnRequestID, ObjectID: req.ObjectID}
	err = inspectDebugNPCOwner(waitContext, host, manager, owner, identity)
	if err != nil {
		return nil, fmt.Errorf("npcBefore: %w", err)
	}
	command, err := debugNPCFeatureRequest(operation, req, identity)
	if err != nil {
		return nil, fmt.Errorf("npcCommand: %w", err)
	}
	if operation == "client.controlled_npc_state" {
		state, stateErr := host.server.DebugControlledNPCState(waitContext, identity)
		result := debugNPCState{ClientID: req.ClientID, InstanceID: req.InstanceID, PID: req.PID, Identity: debugNPCIdentityDTO(state.Identity),
			IsAvailable: state.IsAvailable, UnavailableReason: state.UnavailableReason, IsAIEnabled: state.IsAIEnabled,
			Position: debugWorldPositionDTO(state.Position), HitPoint: state.HitPoint, IsDead: state.IsDead,
			SupportedAbilityNames: append([]string{}, state.SupportedAbilityNames...), ActionGeneration: state.ActionGeneration}
		if state.LastCommand != nil {
			receipt := debugNPCCommandDTO(*state.LastCommand)
			result.LastCommand = &receipt
		}
		if stateErr != nil {
			return result, fmt.Errorf("npcObserve: %w", stateErr)
		}
		if state.Identity != identity {
			return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "npcStateIdentity", Message: "NPC state returned a different owned spawn identity"}
		}
		err = inspectDebugNPCOwner(waitContext, host, manager, owner, identity)
		if err != nil {
			return result, fmt.Errorf("npcAfter: %w", err)
		}
		return result, nil
	}
	receipt, err := host.server.DebugControlNPC(waitContext, command)
	result := debugNPCCommandDTO(receipt)
	if err != nil {
		return result, fmt.Errorf("npcQueue: %w", err)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		result = debugNPCCommandDTO(receipt)
		if receipt.Identity != identity {
			return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "npcReceipt", Message: "NPC command returned a different owned spawn identity"}
		}
		err = inspectDebugNPCOwner(waitContext, host, manager, owner, identity)
		if err != nil {
			return result, fmt.Errorf("npcWaitOwner: %w", err)
		}
		if receipt.Failure != "" || receipt.State == "failed" {
			return result, &control.ScenarioError{Code: control.NotAvailable, Context: "npcFailure", Message: receipt.Failure}
		}
		if receipt.IsSettled && receipt.IsResponseCommitted {
			return result, nil
		}
		select {
		case <-waitContext.Done():
			return result, &control.ScenarioError{Code: control.DeadlineExceeded, Context: "npcWait", Message: "NPC command was not observed settled and response-committed before deadline; it is not replayed", Cause: waitContext.Err()}
		case <-ticker.C:
		}
		receipt, err = host.server.DebugNPCControlState(waitContext, identity, receipt.RequestID)
		if err != nil {
			return result, fmt.Errorf("npcPoll: %w", err)
		}
	}
}

func debugNPCFeatureRequest(operation string, req debugNPCRequest, identity scenario.ControlledNPCIdentity) (scenario.NPCControlRequest, error) {
	command := scenario.NPCControlRequest{Identity: identity}
	switch operation {
	case "client.controlled_npc_state":
		if req.IsAIEnabled != nil || req.Destination != nil || req.AbilityName != "" || req.TargetObjectID != 0 {
			return command, errors.New("NPC state accepts only captured identity")
		}
	case "client.controlled_npc_set_ai":
		if req.IsAIEnabled == nil || req.Destination != nil || req.AbilityName != "" || req.TargetObjectID != 0 {
			return command, errors.New("NPC AI command requires only is_ai_enabled")
		}
		command.Operation, command.IsAIEnabled = "set_ai", req.IsAIEnabled
	case "client.controlled_npc_move":
		if req.IsAIEnabled != nil || req.AbilityName != "" || req.TargetObjectID != 0 {
			return command, errors.New("NPC move requires only a finite destination")
		}
		position, err := debugPathPoint(req.Destination)
		if err != nil {
			return command, fmt.Errorf("npcDestination: %w", err)
		}
		command.Operation, command.Destination = "move", &position
	case "client.controlled_npc_cast":
		if req.IsAIEnabled != nil || req.Destination != nil || req.AbilityName == "" || req.TargetObjectID == 0 {
			return command, errors.New("NPC cast requires an exact supported ability name and target object ID")
		}
		command.Operation, command.AbilityName, command.TargetObjectID = "ability", req.AbilityName, req.TargetObjectID
	case "client.controlled_npc_restore":
		if req.IsAIEnabled != nil || req.Destination != nil || req.AbilityName != "" || req.TargetObjectID != 0 {
			return command, errors.New("NPC restore accepts only captured identity")
		}
		command.Operation = "restore"
	case "client.controlled_npc_retire":
		if req.IsAIEnabled != nil || req.Destination != nil || req.AbilityName != "" || req.TargetObjectID != 0 {
			return command, errors.New("NPC retire accepts only captured identity")
		}
		command.Operation = "retire"
	default:
		return command, errors.New("unsupported controlled NPC operation")
	}
	return command, nil
}
