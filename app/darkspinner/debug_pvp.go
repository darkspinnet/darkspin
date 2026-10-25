//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/scenario"
)

type debugPVPParticipant struct {
	ClientID   string `json:"client_id"`
	InstanceID string `json:"instance_id"`
	PID        uint32 `json:"pid"`
	UserID     int64  `json:"user_id"`
	Team       uint16 `json:"team"`
}

type debugPVPResult struct {
	GameID                     uint32                `json:"game_id"`
	TeamSize                   uint16                `json:"team_size"`
	Participants               []debugPVPParticipant `json:"participants"`
	IsMutationAttempted        bool                  `json:"is_mutation_attempted"`
	IsMutationAccepted         bool                  `json:"is_mutation_accepted"`
	IsProjectionComplete       bool                  `json:"is_projection_complete"`
	NotificationCount          int                   `json:"notification_count"`
	DeliveredNotificationCount int                   `json:"delivered_notification_count"`
	IsNativeAcceptanceObserved bool                  `json:"is_native_acceptance_observed"`
}

// Start an explicit owned-player server fixture through the production arena
// formation and client projections. It does not pretend to exercise native
// matchmaking input or to prove that participants have loaded the arena.
func startDebugPVP(ctx context.Context, host *debugHost, clientManager *debugClientManager, payload json.RawMessage) (debugPVPResult, error) {
	result := debugPVPResult{Participants: []debugPVPParticipant{}}
	if ctx == nil || host == nil || host.server == nil || clientManager == nil {
		return result, errors.New("PvP scenario requires an owned server and clients")
	}
	var req struct {
		Teams [][]string `json:"teams"`
	}
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return result, fmt.Errorf("pvpRequest: %w", err)
	}
	if len(req.Teams) != 2 || len(req.Teams[0]) < 1 || len(req.Teams[0]) > 2 || len(req.Teams[0]) != len(req.Teams[1]) {
		return result, errors.New("PvP requires two equal teams of one or two owned clients")
	}
	result.TeamSize = uint16(len(req.Teams[0]))
	command := scenario.PVPRequest{TeamSize: result.TeamSize}
	owners := make([]debugWorldOwner, 0, 2*len(req.Teams[0]))
	seenUserIDs := make(map[int64]bool)
	seenClientIDs := make(map[string]bool)
	for teamIndex, clientIDs := range req.Teams {
		for _, clientID := range clientIDs {
			if seenClientIDs[clientID] {
				return result, errors.New("PvP participants must be distinct owned clients")
			}
			owner, ownerErr := observeDebugWorldOwner(ctx, host, clientManager, clientID)
			if ownerErr != nil {
				return result, fmt.Errorf("pvpOwner: %w", ownerErr)
			}
			if owner.profile.GameID != 0 || seenUserIDs[owner.profile.UserID] {
				return result, errors.New("PvP requires distinct authenticated accounts outside active games")
			}
			seenClientIDs[clientID] = true
			seenUserIDs[owner.profile.UserID] = true
			owners = append(owners, owner)
			team := uint16(teamIndex + 1)
			command.Participants = append(command.Participants, scenario.PVPParticipant{UserID: owner.profile.UserID, SessionID: owner.profile.SessionID, Team: team})
			command.OwnedUserIDs = append(command.OwnedUserIDs, owner.profile.UserID)
			result.Participants = append(result.Participants, debugPVPParticipant{ClientID: clientID, InstanceID: owner.client.InstanceID, PID: owner.client.ProcessID, UserID: owner.profile.UserID, Team: team})
		}
	}
	for _, owner := range owners {
		err = recheckDebugWorldOwner(ctx, host, clientManager, owner)
		if err != nil {
			return result, fmt.Errorf("pvpRecheck: %w", err)
		}
	}
	receipt, operationErr := host.server.DebugPVP(ctx, command)
	result.GameID = receipt.GameID
	result.IsMutationAttempted = receipt.IsMutationAttempted
	result.IsMutationAccepted = receipt.IsMutationAccepted
	result.IsProjectionComplete = receipt.IsProjectionComplete
	result.NotificationCount = receipt.NotificationCount
	result.DeliveredNotificationCount = receipt.DeliveredNotificationCount
	if operationErr != nil {
		return result, fmt.Errorf("pvpStart: %w", operationErr)
	}
	for _, owner := range owners {
		err = recheckDebugPartyClient(clientManager, owner.client)
		if err != nil {
			return result, fmt.Errorf("pvpClientFinal: %w", err)
		}
	}
	return result, nil
}
