//go:build scenario

package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/party"
	"github.com/darkspinnet/darkspin/server/scenario"
)

type debugPartyProvider interface {
	DebugParty(context.Context, scenario.PartyRequest) (scenario.PartyResult, error)
}

// DebugParty accepts only explicit owned identities supplied by the development
// supervisor. Business authority stays with party.Service; client projection
// stays with the registered production Blaze handler on its actor request stage.
func (e *Server) DebugParty(ctx context.Context, req scenario.PartyRequest) (scenario.PartyResult, error) {
	result := scenario.PartyResult{Operation: req.Operation, ActorID: req.ActorID}
	if e == nil || e.partyService == nil || ctx == nil {
		return result, errors.New("development party service is unavailable")
	}
	ownedUsers := make(map[int64]bool, len(req.OwnedUserIDs))
	for _, userID := range req.OwnedUserIDs {
		ownedUsers[userID] = true
	}
	if req.ActorID <= 0 || !ownedUsers[req.ActorID] {
		return result, errors.New("party actor must be an owned client account")
	}
	actor, err := e.ScenarioObservation(ctx, req.ActorID, scenario.Authenticated)
	if err != nil {
		return result, fmt.Errorf("partyAuthenticate: %w", err)
	}
	if actor.Outcome != scenario.Passed {
		return result, errors.New("party actor has no unique accepted authentication")
	}
	req.ActorSessionID = actor.SessionID
	result.ActorSessionID = actor.SessionID
	before, isBefore := e.partyService.PartyForMember(req.ActorID)
	if isBefore {
		result.State = debugPartyState(before)
		err = validateDebugPartyMembers(before, ownedUsers)
		if err != nil {
			return result, fmt.Errorf("partyMembers: %w", err)
		}
	}
	switch req.Operation {
	case "party.state":
		// Provider validates pinned loopback ownership only; no handler or mutation.
	case "party.create":
		if isBefore {
			return result, errors.New("owned actor already has a party; inspect party.state instead of replaying creation")
		}
		if len(req.Name) > 128 {
			return result, errors.New("party name exceeds 128 bytes")
		}
	case "party.invite":
		if !isBefore {
			return result, errors.New("party inviter has no current party")
		}
		if req.InviteeID <= 0 || req.InviteeID == req.ActorID || !ownedUsers[req.InviteeID] {
			return result, errors.New("party invitee must be a different owned client")
		}
		invitee, inviteErr := e.ScenarioObservation(ctx, req.InviteeID, scenario.Authenticated)
		if inviteErr != nil {
			return result, fmt.Errorf("partyInviteAuth: %w", inviteErr)
		}
		if invitee.Outcome != scenario.Passed {
			return result, errors.New("party invitee has no unique accepted authentication")
		}
		req.InviteeSessionID = invitee.SessionID
		req.PartyID = before.ID
	case "party.join":
		if isBefore {
			return result, errors.New("owned actor already has party membership; join cannot be replayed")
		}
		if req.LeaderID <= 0 || req.LeaderID == req.ActorID || !ownedUsers[req.LeaderID] {
			return result, errors.New("party join requires a different owned leader")
		}
		leader, leaderErr := e.ScenarioObservation(ctx, req.LeaderID, scenario.Authenticated)
		if leaderErr != nil {
			return result, fmt.Errorf("partyLeaderAuth: %w", leaderErr)
		}
		if leader.Outcome != scenario.Passed {
			return result, errors.New("party leader has no unique accepted authentication")
		}
		leaderParty, isLeaderParty := e.partyService.PartyForMember(req.LeaderID)
		if !isLeaderParty || leaderParty.LeaderID != req.LeaderID {
			return result, errors.New("selected owned client is not an authoritative party leader")
		}
		err = validateDebugPartyMembers(leaderParty, ownedUsers)
		if err != nil {
			return result, fmt.Errorf("partyLeaderMembers: %w", err)
		}
		req.LeaderSessionID = leader.SessionID
		req.PartyID = leaderParty.ID
	default:
		return result, errors.New("unsupported development party operation")
	}
	providers := make([]debugPartyProvider, 0, len(e.blazeServers)+len(e.sharedTCPServers))
	for _, provider := range e.blazeServers {
		providers = append(providers, provider)
	}
	for _, provider := range e.sharedTCPServers {
		providers = append(providers, provider)
	}
	for _, provider := range providers {
		receipt, operationErr := provider.DebugParty(ctx, req)
		if errors.Is(operationErr, scenario.ErrPartySessionMissing) && !receipt.IsMutationAttempted {
			continue
		}
		current, isCurrent := e.partyService.PartyForMember(req.ActorID)
		if isCurrent {
			receipt.State = debugPartyState(current)
		}
		if operationErr != nil {
			return receipt, fmt.Errorf("partyProject: %w", operationErr)
		}
		return receipt, nil
	}
	return result, errors.New("owned party actor has no available loopback Blaze provider")
}

func validateDebugPartyMembers(snapshot party.Snapshot, ownedUsers map[int64]bool) error {
	for _, member := range snapshot.Members {
		if !ownedUsers[member.ID] {
			return errors.New("party contains a participant outside the owned client allowlist")
		}
	}
	return nil
}

func debugPartyState(snapshot party.Snapshot) scenario.PartyState {
	result := scenario.PartyState{ID: snapshot.ID, Name: snapshot.Name, LeaderID: snapshot.LeaderID,
		Revision: snapshot.Revision, Members: make([]scenario.PartyMember, 0, len(snapshot.Members))}
	for _, member := range snapshot.Members {
		result.Members = append(result.Members, scenario.PartyMember{UserID: member.ID, Name: member.Name, AvatarID: member.AvatarID, JoinOrdinal: member.JoinOrdinal})
	}
	return result
}
