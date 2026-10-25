//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/scenario"
)

type debugPartyRequest struct {
	ClientID        string `json:"client_id"`
	Name            string `json:"name,omitempty"`
	InviteeClientID string `json:"invitee_client_id,omitempty"`
	LeaderClientID  string `json:"leader_client_id,omitempty"`
}

type debugPartyMember struct {
	UserID      int64  `json:"user_id"`
	Name        string `json:"name"`
	AvatarID    uint32 `json:"avatar_id"`
	JoinOrdinal uint32 `json:"join_ordinal"`
}

type debugPartyState struct {
	ID           uint32             `json:"id"`
	Name         string             `json:"name"`
	LeaderUserID int64              `json:"leader_user_id"`
	Revision     uint64             `json:"revision"`
	Members      []debugPartyMember `json:"members"`
}

type debugPartyResult struct {
	Operation                  string          `json:"operation"`
	ClientID                   string          `json:"client_id"`
	InstanceID                 string          `json:"instance_id"`
	PID                        uint32          `json:"pid"`
	IsMutationAttempted        bool            `json:"is_mutation_attempted"`
	IsMutationAccepted         bool            `json:"is_mutation_accepted"`
	IsProjectionComplete       bool            `json:"is_projection_complete"`
	IsNativeAcceptanceObserved bool            `json:"is_native_acceptance_observed"`
	NotificationCount          int             `json:"notification_count"`
	DeliveredNotificationCount int             `json:"delivered_notification_count"`
	Party                      debugPartyState `json:"party"`
}

// executeDebugPartyOperation authorizes fresh profiles of live owned processes.
// It invokes one production feature/projection operation without mutation replay.
func executeDebugPartyOperation(ctx context.Context, host *debugHost, clientManager *debugClientManager, operation string, payload json.RawMessage) (any, error) {
	if ctx == nil || host == nil || host.server == nil || clientManager == nil {
		return nil, errors.New("party operation requires an owned host and client manager")
	}
	var req debugPartyRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return nil, fmt.Errorf("partyDecode: %w", err)
	}
	switch operation {
	case "party.state":
		if req.Name != "" || req.InviteeClientID != "" || req.LeaderClientID != "" {
			return nil, errors.New("party.state accepts only client_id")
		}
	case "party.create":
		if req.InviteeClientID != "" || req.LeaderClientID != "" {
			return nil, errors.New("party.create accepts only client_id and name")
		}
	case "party.invite":
		if req.Name != "" || req.LeaderClientID != "" || req.InviteeClientID == "" {
			return nil, errors.New("party.invite requires client_id and invitee_client_id")
		}
	case "party.join":
		if req.Name != "" || req.InviteeClientID != "" || req.LeaderClientID == "" {
			return nil, errors.New("party.join requires client_id and leader_client_id")
		}
	default:
		return nil, errors.New("unsupported development party operation")
	}
	actor, actorID, err := resolveDebugPartyClient(host, clientManager, req.ClientID)
	if err != nil {
		return nil, fmt.Errorf("partyActor: %w", err)
	}
	command := scenario.PartyRequest{Operation: operation, ActorID: actorID, Name: req.Name}
	var target debugClientState
	if req.InviteeClientID != "" || req.LeaderClientID != "" {
		targetID := req.InviteeClientID
		if req.LeaderClientID != "" {
			targetID = req.LeaderClientID
		}
		var userID int64
		target, userID, err = resolveDebugPartyClient(host, clientManager, targetID)
		if err != nil {
			return nil, fmt.Errorf("partyTarget: %w", err)
		}
		if target.ClientID == actor.ClientID || userID == actorID {
			return nil, errors.New("party target must be a different owned client and account")
		}
		if req.InviteeClientID != "" {
			command.InviteeID = userID
		} else {
			command.LeaderID = userID
		}
	}
	ownedUsers := make(map[int64]bool)
	for _, client := range clientManager.List() {
		// Observation must retain an owned member while its process reconnects.
		// Mutations still require every allowed participant to be live.
		if !isDebugPartyClientLive(client) && operation != "party.state" {
			continue
		}
		credential, credentialErr := host.ProfileCredential(client.ProfileName)
		if credentialErr != nil {
			return nil, fmt.Errorf("partyOwnedProfile: %w", credentialErr)
		}
		if credential.UserID <= 0 {
			return nil, errors.New("owned party profile has no account identity")
		}
		if credential.UserID != client.UserID {
			return nil, errors.New("owned party profile no longer matches the launch account")
		}
		if !ownedUsers[credential.UserID] {
			ownedUsers[credential.UserID] = true
			command.OwnedUserIDs = append(command.OwnedUserIDs, credential.UserID)
		}
	}
	receipt, operationErr := host.server.DebugParty(ctx, command)
	result := mapDebugPartyResult(actor, receipt)
	actorErr := recheckDebugPartyClient(clientManager, actor)
	var targetErr error
	if target.ClientID != "" {
		targetErr = recheckDebugPartyClient(clientManager, target)
	}
	if operationErr != nil || actorErr != nil || targetErr != nil {
		return result, fmt.Errorf("partyOperation: %w", errors.Join(operationErr, actorErr, targetErr))
	}
	return result, nil
}

func isDebugPartyClientLive(client debugClientState) bool {
	return client.ClientID != "" && client.InstanceID != "" && client.ProcessID != 0 && client.ProfileName != "" && client.IsRunning && client.Status != "liveness_unavailable"
}

func resolveDebugPartyClient(host *debugHost, clientManager *debugClientManager, clientID string) (debugClientState, int64, error) {
	if clientID == "" {
		return debugClientState{}, 0, errors.New("party operation requires a named owned client")
	}
	client, err := clientManager.State(clientID)
	if err != nil {
		return client, 0, fmt.Errorf("partyClientState: %w", err)
	}
	if !isDebugPartyClientLive(client) {
		return client, 0, errors.New("party client must have a live owned process and profile")
	}
	credential, err := host.ProfileCredential(client.ProfileName)
	if err != nil {
		return client, 0, fmt.Errorf("partyProfile: %w", err)
	}
	if credential.UserID <= 0 {
		return client, 0, errors.New("party profile has no owned account identity")
	}
	return client, credential.UserID, nil
}

func recheckDebugPartyClient(clientManager *debugClientManager, before debugClientState) error {
	current, err := clientManager.State(before.ClientID)
	if err != nil {
		return fmt.Errorf("partyClientRecheck: %w", err)
	}
	if !isDebugPartyClientLive(current) || current.InstanceID != before.InstanceID || current.ProcessID != before.ProcessID || current.ProfileName != before.ProfileName {
		return errors.New("owned party client changed during processing; do not replay the mutation")
	}
	return nil
}

func mapDebugPartyResult(client debugClientState, receipt scenario.PartyResult) debugPartyResult {
	result := debugPartyResult{Operation: receipt.Operation, ClientID: client.ClientID, InstanceID: client.InstanceID, PID: client.ProcessID,
		IsMutationAttempted: receipt.IsMutationAttempted, IsMutationAccepted: receipt.IsMutationAccepted,
		IsProjectionComplete: receipt.IsProjectionComplete, IsNativeAcceptanceObserved: receipt.IsNativeAcceptanceObserved,
		NotificationCount: receipt.NotificationCount, DeliveredNotificationCount: receipt.DeliveredNotificationCount,
		Party: debugPartyState{ID: receipt.State.ID, Name: receipt.State.Name, LeaderUserID: receipt.State.LeaderID, Revision: receipt.State.Revision, Members: make([]debugPartyMember, 0, len(receipt.State.Members))}}
	for _, member := range receipt.State.Members {
		result.Party.Members = append(result.Party.Members, debugPartyMember{UserID: member.UserID, Name: member.Name, AvatarID: member.AvatarID, JoinOrdinal: member.JoinOrdinal})
	}
	return result
}
