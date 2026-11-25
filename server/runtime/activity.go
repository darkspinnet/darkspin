package runtime

import (
	"context"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/pvp"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

func (e *Server) runActivitySampling(ctx context.Context) {
	defer close(e.activityDone)
	e.recordActivity(ctx)
	for {
		nextHour := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
		timer := time.NewTimer(time.Until(nextHour))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			e.recordActivity(ctx)
		}
	}
}

func (e *Server) recordActivity(ctx context.Context) {
	if e.activity == nil || ctx.Err() != nil {
		return
	}
	users := e.userManager.Users()
	players := make([]activity.Player, 0, len(users))
	usersByID := make(map[int64]*sporenet.User, len(users))
	for _, user := range users {
		userID, level := user.ActivityView()
		if userID <= 0 {
			continue
		}
		if _, isFound := usersByID[userID]; isFound {
			continue
		}
		usersByID[userID] = user
		players = append(players, activity.Player{UserID: userID, Level: level})
	}
	observationCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	at := time.Now()
	err := e.activity.Record(observationCtx, activity.Observation{
		At: at, Players: players, Queues: e.activityQueues(at),
		Distribution: e.activityDistribution(usersByID),
	})
	if err != nil && ctx.Err() == nil {
		e.logger.Printf("Player activity sample failed: %v", err)
	}
}

func (e *Server) activityQueues(at time.Time) []activity.QueueObservation {
	if e.matchmaking == nil {
		return nil
	}
	queueObservations := e.matchmaking.ObserveQueues(at)
	observations := make([]activity.QueueObservation, 0, len(queueObservations))
	for _, queueObservation := range queueObservations {
		kind := activityQueueKind(queueObservation.Criteria)
		if kind == "" {
			continue
		}
		observations = append(observations, activity.QueueObservation{
			Kind: kind, QueuedCount: queueObservation.QueuedCount,
			WaitSeconds:        queueObservation.WaitSeconds,
			MaximumWaitSeconds: queueObservation.MaximumWaitSeconds,
		})
	}
	return observations
}

func activityQueueKind(criteria pvp.MatchmakingCriteria) activity.QueueKind {
	if criteria.GameType == pvp.CampaignGameType {
		return activity.QueueCampaign
	}
	if criteria.GameType != pvp.ArenaGameType {
		return ""
	}
	switch criteria.TeamSize {
	case pvp.ArenaOneVersusOneTeamSize:
		if criteria.IsRanked {
			return activity.QueueArena1Ranked
		}
		return activity.QueueArena1Unranked
	case pvp.ArenaTeamSize:
		if criteria.IsRanked {
			return activity.QueueArena2Ranked
		}
		return activity.QueueArena2Unranked
	default:
		return ""
	}
}

func (e *Server) activityDistribution(usersByID map[int64]*sporenet.User) *activity.Distribution {
	if e.gameManager == nil || e.matchmaking == nil {
		return nil
	}
	distribution := &activity.Distribution{}
	for userID, user := range usersByID {
		gameID := user.CurrentGameID()
		var mode game.Mode
		var state game.SessionState
		var isMember bool
		if gameID != 0 {
			instance := e.gameManager.Game(gameID)
			mode, state, isMember = instance.PlayerActivity(userID)
		}
		isKnownMode := mode == game.ModeChain || mode == game.ModeTutorial || mode == game.ModeArena
		if isMember && isKnownMode && state == game.StateInGame {
			if mode == game.ModeArena {
				distribution.PVPCount++
			} else {
				distribution.PVECount++
			}
			continue
		}
		if e.matchmaking.IsQueued(userID) {
			distribution.MatchmakingCount++
			continue
		}
		if gameID == 0 || isMember && isKnownMode && isIdleActivityState(state) {
			distribution.LobbyCount++
			continue
		}
		distribution.OtherCount++
	}
	return distribution
}

func isIdleActivityState(state game.SessionState) bool {
	switch state {
	case game.StateNew, game.StateInitializing, game.StatePreGame,
		game.StatePostGame, game.StateResettable, game.StateReplaySetup:
		return true
	default:
		return false
	}
}
