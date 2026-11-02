package blaze

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/pvp"
)

const matchmakingBotFillDelay = 30 * time.Second

// SetMatchmakingConfig supplies launcher-owned settings for bot-filled games.
func (e *Registry) SetMatchmakingConfig(config *game.Config) {
	e.mu.Lock()
	e.matchmakingConfig = config
	e.mu.Unlock()
}

func (e *Registry) campaignMatchmakingBotSize() (uint32, error) {
	e.mu.RLock()
	config := e.matchmakingConfig
	e.mu.RUnlock()
	if config == nil {
		return 1, nil
	}
	botSize, err := config.CampaignMatchmakingBotSize()
	if err != nil {
		return 0, fmt.Errorf("botSize: %w", err)
	}
	return botSize, nil
}

func (e *Registry) isCampaignBotSquadMirroringEnabled() bool {
	e.mu.RLock()
	config := e.matchmakingConfig
	e.mu.RUnlock()
	return config == nil || config.Bool(game.ConfigIsCampaignBotSquadMirroringEnabled)
}

func isBotFillCriteria(criteria pvp.MatchmakingCriteria) bool {
	if criteria.IsRanked {
		return false
	}
	return criteria.GameType == pvp.CampaignGameType || pvp.IsArenaCriteria(criteria)
}

func authenticatedHumanCount(server *Server) int {
	server.mu.Lock()
	defer server.mu.Unlock()
	count := 0
	for userID, sessions := range server.sessionsByUser {
		if userID > 0 && len(sessions) != 0 {
			count++
		}
	}
	return count
}

type matchmakingBotFill struct {
	matchmaking   *pvp.Matchmaking
	gameManager   *game.Manager
	userDirectory activeUserDirectory
	server        *Server
	session       pvp.MatchmakingSession
}

func (e *matchmakingBotFill) wait(ctx context.Context) {
	timer := time.NewTimer(matchmakingBotFillDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		e.matchmaking.Cancel(e.session.OwnerID, e.session.ID)
		return
	case <-timer.C:
	}
	err := e.complete(ctx)
	if err != nil {
		e.server.logger.Printf("matchmaking bot fill session_id=%d failed: %v", e.session.ID, err)
	}
}

func (e *matchmakingBotFill) complete(ctx context.Context) error {
	registry := e.server.registry
	err := registry.lockPartyLifecycle(ctx)
	if err != nil {
		return fmt.Errorf("fillLock: %w", err)
	}
	request, err := e.prepare()
	registry.partyLifecycle.Unlock()
	if err != nil {
		return fmt.Errorf("fillPrepare: %w", err)
	}
	if request == nil {
		return nil
	}
	for _, notification := range request.targetedNotifications {
		target := notification.session
		target.traceFrame("out", notification.frame)
		err = target.send(notification.frame)
		if err != nil {
			e.server.logger.Printf("matchmaking bot fill notify session_id=%d failed: %v", target.ID, err)
		}
	}
	return nil
}

// prepare runs under the same lifecycle gate as cancellation, disconnect and
// ordinary matchmaking. It builds a fresh notification request from live sessions.
func (e *matchmakingBotFill) prepare() (*Request, error) {
	for _, session := range e.matchmaking.PartialSessions(e.session.ID) {
		user := e.userDirectory.UserByID(session.UserID)
		if user == nil || user.CurrentGameID() != 0 || len(e.server.sessionsForUser(session.UserID)) != 1 {
			e.matchmaking.Cancel(session.OwnerID, session.ID)
		}
	}
	sessions := e.matchmaking.ClaimPartial(e.session.ID, matchmakingBotFillDelay)
	if len(sessions) == 0 {
		return nil, nil
	}
	playerSessions := e.server.sessionsForUser(sessions[0].UserID)
	if len(playerSessions) != 1 {
		return nil, ErrSessionUnavailable
	}
	request := &Request{Session: playerSessions[0], Component: GameManagerComponentID, Command: 0x0d}
	err := queueMatchmakingFormation(request, e.gameManager, e.userDirectory,
		sessions, e.session.Criteria, true)
	if err != nil {
		return nil, fmt.Errorf("fillFormation: %w", err)
	}
	return request, nil
}

func queueMatchmakingFormation(
	request *Request, gameManager *game.Manager, userDirectory activeUserDirectory,
	sessions []pvp.MatchmakingSession, criteria pvp.MatchmakingCriteria, isBotFill bool,
) error {
	var instance *game.Instance
	var formErr error
	if isBotFill {
		botSize := uint32(1)
		isSquadMirroringEnabled := true
		if criteria.GameType == pvp.CampaignGameType {
			botSize, formErr = request.Session.server.registry.campaignMatchmakingBotSize()
			isSquadMirroringEnabled = request.Session.server.registry.isCampaignBotSquadMirroringEnabled()
		}
		if formErr == nil {
			instance, formErr = formBotFilledGame(gameManager, userDirectory, sessions, criteria, botSize, isSquadMirroringEnabled)
		}
	} else {
		instance, formErr = formMatchmakingGame(gameManager, userDirectory, sessions, criteria)
	}
	if formErr != nil {
		err := queueMatchmakingFailures(request, sessions)
		if err != nil {
			return fmt.Errorf("matchFailureNotify: %w", err)
		}
		logGameRequest(request, "matchmaking_form session_id=%d result=%q error=%q",
			sessions[0].ID, "rejected", formErr)
		return nil
	}
	err := queueGameSetupRoster(request, instance, gameSetupContextMatchmaking, sessions)
	if err != nil {
		gameManager.Remove(instance.ID)
		return fmt.Errorf("matchRoster: %w", err)
	}
	if instance.Info.IsWarped {
		for _, session := range sessions {
			gameManager.ConsumeCampaignWarp(session.UserID, instance.Info.Level)
		}
	}
	logGameRequest(request,
		"matchmaking_form session_id=%d game_id=%d level=%q result=%q players=%d bot_fill=%t",
		sessions[0].ID, instance.ID, instance.Info.Level, "matched", len(sessions), isBotFill)
	return nil
}
