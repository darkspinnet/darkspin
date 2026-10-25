//go:build scenario

package blaze

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/pvp"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

// DebugPVP is an explicitly authorized fixture, not a native matchmaking RPC.
// It reuses production arena formation and setup notifications without sending
// a fabricated RPC response or claiming that any client accepted the setup.
func (e *Server) DebugPVP(ctx context.Context, req scenario.PVPRequest, gameManager *game.Manager, userManager *sporenet.UserManager) (scenario.PVPResult, error) {
	result := scenario.PVPResult{Participants: append([]scenario.PVPParticipant(nil), req.Participants...)}
	if ctx == nil || gameManager == nil || userManager == nil {
		return result, errors.New("PvP fixture dependencies are unavailable")
	}
	if req.TeamSize == 3 {
		return result, errors.New("3v3 exceeds the current four-player server capacity")
	}
	if (req.TeamSize != 1 && req.TeamSize != 2) || len(req.Participants) != 2*int(req.TeamSize) {
		return result, errors.New("PvP fixture supports explicit 1v1 or 2v2 teams")
	}
	ownedUsers := make(map[int64]bool, len(req.OwnedUserIDs))
	for _, userID := range req.OwnedUserIDs {
		ownedUsers[userID] = true
	}
	sessions := make([]*Session, 0, len(req.Participants))
	sessionsByUsers := make(map[int64]*Session, len(req.Participants))
	teamCounts := make(map[uint16]int, 2)
	for _, participant := range req.Participants {
		if !ownedUsers[participant.UserID] || participant.Team < 1 || participant.Team > 2 || sessionsByUsers[participant.UserID] != nil {
			return result, errors.New("PvP fixture requires distinct owned accounts with explicit teams")
		}
		session, err := e.debugPartySession(ctx, participant.UserID, participant.SessionID)
		if errors.Is(err, scenario.ErrPartySessionMissing) {
			return result, fmt.Errorf("pvpBinding: %w", scenario.ErrPVPSessionMissing)
		}
		if err != nil {
			return result, fmt.Errorf("pvpParticipant: %w", err)
		}
		sessionsByUsers[participant.UserID] = session
		sessions = append(sessions, session)
		teamCounts[participant.Team]++
	}
	if teamCounts[1] != int(req.TeamSize) || teamCounts[2] != int(req.TeamSize) {
		return result, errors.New("PvP fixture requires balanced teams")
	}
	// Stable lock order serializes concurrent fixtures with live RPC operations.
	sort.Slice(sessions, func(firstIndex, secondIndex int) bool {
		return sessions[firstIndex].ID < sessions[secondIndex].ID
	})
	lockedCount := 0
	defer unlockDebugPVPSessions(sessions, &lockedCount)
	for _, session := range sessions {
		err := session.lockOperation(ctx)
		if err != nil {
			return result, fmt.Errorf("pvpStage: %w", err)
		}
		lockedCount++
	}
	for _, participant := range req.Participants {
		current, err := e.debugPartySession(ctx, participant.UserID, participant.SessionID)
		if err != nil {
			return result, fmt.Errorf("pvpRecheck: %w", err)
		}
		if current != sessionsByUsers[participant.UserID] {
			return result, errors.New("PvP participant session changed before execution")
		}
		user := requestUser(&Request{Session: current})
		if user == nil || user.CurrentGameID() != 0 || user.CurrentPlaygroupID() != 0 {
			return result, errors.New("PvP fixture requires owned clients outside existing games and parties")
		}
		err = validateDebugPVPSquad(user.View())
		if err != nil {
			return result, fmt.Errorf("pvpDeck[%d]: %w", participant.UserID, err)
		}
	}
	criteria := pvp.MatchmakingCriteria{GameType: pvp.ArenaGameType, ExpectedPlayerCount: 2 * req.TeamSize, TeamSize: req.TeamSize}
	matchmakingSessions := make([]pvp.MatchmakingSession, 0, len(req.Participants))
	for team := uint16(1); team <= 2; team++ {
		for _, participant := range req.Participants {
			if participant.Team != team {
				continue
			}
			matchmakingSessions = append(matchmakingSessions, pvp.MatchmakingSession{UserID: participant.UserID, OwnerID: participant.UserID, Criteria: criteria})
		}
	}
	err := ctx.Err()
	if err != nil {
		return result, fmt.Errorf("pvpContext: %w", err)
	}
	result.IsMutationAttempted = true
	instance, err := formArenaGame(gameManager, userManager, matchmakingSessions, criteria)
	if err != nil {
		return result, fmt.Errorf("pvpForm: %w", err)
	}
	result.GameID = instance.ID
	result.IsMutationAccepted = true
	actor := sessionsByUsers[req.Participants[0].UserID]
	request := &Request{Session: actor, ownedPartyUsers: ownedUsers}
	err = queueGameSetupRoster(request, instance, gameSetupContextIndirectJoin, nil)
	if err != nil {
		// No queued projection has been delivered, so formation can be retired.
		gameManager.Remove(instance.ID)
		return result, fmt.Errorf("pvpSetup: %w", err)
	}
	projection := scenario.PartyResult{}
	err = actor.deliverDebugParty(ctx, request, &projection)
	result.NotificationCount = projection.NotificationCount
	result.DeliveredNotificationCount = projection.DeliveredNotificationCount
	result.IsProjectionComplete = err == nil && projection.NotificationCount == len(req.Participants) && projection.DeliveredNotificationCount == len(req.Participants)
	if err != nil {
		// A partial delivery must remain visible; never silently replay setup.
		return result, fmt.Errorf("pvpDelivery: %w", err)
	}
	if !result.IsProjectionComplete {
		return result, errors.New("PvP setup did not project to every pinned participant")
	}
	return result, nil
}

func validateDebugPVPSquad(snapshot sporenet.UserView) error {
	var selectedSquad sporenet.Squad
	squadCount := 0
	for _, squad := range snapshot.Squads {
		if squad.ID != snapshot.Account.DefaultDeckPVPID {
			continue
		}
		selectedSquad = squad
		squadCount++
	}
	if squadCount != 1 || selectedSquad.Category != "pvp" || selectedSquad.IsLockedFor(snapshot.Account) {
		return errors.New("PvP fixture requires one selected unlocked PvP squad; enable PvP when creating the scenario profile")
	}
	selectedIDs := make(map[uint32]bool, len(selectedSquad.CreatureIDs))
	for _, creatureID := range selectedSquad.CreatureIDs {
		if creatureID == 0 || selectedIDs[creatureID] {
			return errors.New("PvP fixture squad requires three distinct owned heroes")
		}
		creatureCount := 0
		for _, creature := range snapshot.Creatures {
			if creature == nil || creature.ID != creatureID {
				continue
			}
			if creature.Noun() == 0 {
				return errors.New("PvP fixture squad contains a hero without a valid template noun")
			}
			creatureCount++
		}
		if creatureCount != 1 {
			return errors.New("PvP fixture squad has a missing or ambiguous owned hero")
		}
		selectedIDs[creatureID] = true
	}
	return nil
}

func unlockDebugPVPSessions(sessions []*Session, lockedCount *int) {
	for sessionIndex := *lockedCount - 1; sessionIndex >= 0; sessionIndex-- {
		sessions[sessionIndex].operationMutex.Unlock()
	}
}
