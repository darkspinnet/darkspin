//go:build scenario

package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

type debugPVPProvider interface {
	DebugPVP(context.Context, scenario.PVPRequest, *game.Manager, *sporenet.UserManager) (scenario.PVPResult, error)
}

// DebugPVP forms a local scenario arena for explicit owned clients. Formation
// uses the production arena path; delivery is not native acceptance evidence.
func (e *Server) DebugPVP(ctx context.Context, req scenario.PVPRequest) (scenario.PVPResult, error) {
	result := scenario.PVPResult{Participants: append([]scenario.PVPParticipant(nil), req.Participants...)}
	if e == nil || e.gameManager == nil || e.userManager == nil || ctx == nil {
		return result, errors.New("development PvP service is unavailable")
	}
	if req.TeamSize == 3 {
		return result, errors.New("3v3 scenarios exceed the current four-player server capacity")
	}
	if (req.TeamSize != 1 && req.TeamSize != 2) || len(req.Participants) != 2*int(req.TeamSize) {
		return result, errors.New("PvP scenario requires two participants for 1v1 or four participants for 2v2")
	}
	ownedUsers := make(map[int64]bool, len(req.OwnedUserIDs))
	for _, userID := range req.OwnedUserIDs {
		ownedUsers[userID] = true
	}
	participants := make([]scenario.PVPParticipant, 0, len(req.Participants))
	seenUsers := make(map[int64]bool, len(req.Participants))
	teamCounts := make(map[uint16]int, 2)
	for _, participant := range req.Participants {
		if participant.UserID <= 0 || !ownedUsers[participant.UserID] || seenUsers[participant.UserID] {
			return result, errors.New("PvP participants must be distinct owned client accounts")
		}
		if participant.SessionID == "" || participant.Team < 1 || participant.Team > 2 {
			return result, errors.New("PvP participants require pinned sessions and explicit team 1 or 2")
		}
		observation, err := e.ScenarioObservation(ctx, participant.UserID, scenario.Authenticated)
		if err != nil {
			return result, fmt.Errorf("pvpAuthenticate: %w", err)
		}
		if observation.Outcome != scenario.Passed || observation.SessionID != participant.SessionID {
			return result, errors.New("PvP participant has no unique matching accepted authentication")
		}
		seenUsers[participant.UserID] = true
		teamCounts[participant.Team]++
		participants = append(participants, participant)
	}
	if teamCounts[1] != int(req.TeamSize) || teamCounts[2] != int(req.TeamSize) {
		return result, errors.New("PvP scenario requires balanced explicit teams")
	}
	err := e.validateDebugPVPParties(participants, seenUsers, req.TeamSize)
	if err != nil {
		return result, fmt.Errorf("pvpParties: %w", err)
	}
	req.Participants = participants
	providers := make([]debugPVPProvider, 0, len(e.blazeServers)+len(e.sharedTCPServers))
	for _, provider := range e.blazeServers {
		providers = append(providers, provider)
	}
	for _, provider := range e.sharedTCPServers {
		providers = append(providers, provider)
	}
	for _, provider := range providers {
		receipt, err := provider.DebugPVP(ctx, req, e.gameManager, e.userManager)
		if errors.Is(err, scenario.ErrPVPSessionMissing) && !receipt.IsMutationAttempted {
			continue
		}
		if err != nil {
			return receipt, fmt.Errorf("pvpProject: %w", err)
		}
		return receipt, nil
	}
	return result, errors.New("owned PvP participants have no shared available loopback Blaze provider")
}

func (e *Server) validateDebugPVPParties(
	participants []scenario.PVPParticipant, selectedUsers map[int64]bool, teamSize uint16,
) error {
	if e.partyService == nil {
		return errors.New("PvP party service unavailable")
	}
	duelPartyID := uint32(0)
	for _, participant := range participants {
		partySS, isPartyFound := e.partyService.PartyForMember(participant.UserID)
		if !isPartyFound || partySS.ID == 0 {
			return errors.New("PvP requires established party membership before starting")
		}
		for _, member := range partySS.Members {
			if !selectedUsers[member.ID] {
				return errors.New("PvP party contains a member outside the selected owned participants")
			}
		}
		if teamSize != 1 {
			continue
		}
		if len(partySS.Members) != 2 || duelPartyID != 0 && duelPartyID != partySS.ID {
			return errors.New("1v1 requires both participants in the same two-player party")
		}
		duelPartyID = partySS.ID
	}
	return nil
}
