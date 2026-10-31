package gameplay

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

type arenaStatEntry struct {
	userID uint64
	result sporenet.PVPMatchResult
}

type arenaStatMaximums struct {
	damage  float32
	healing float32
}

func newArenaMatchID() (string, error) {
	identity := make([]byte, 16)
	count, err := rand.Read(identity)
	if err != nil {
		return "", fmt.Errorf("arenaIdentityRead: %w", err)
	}
	if count != len(identity) {
		return "", errors.New("incomplete Arena match identity")
	}
	return hex.EncodeToString(identity), nil
}

func (e *arenaResultState) freezeStats(completedAt time.Time) {
	if !e.isCompleted || len(e.statEntries) != 0 {
		return
	}
	for userID, player := range e.players {
		if e.botUserIDs[userID] {
			continue
		}
		wins, losses := e.body.CountA, e.body.CountB
		if player.Team == 2 {
			wins, losses = losses, wins
		}
		maximums := e.statMaximums[userID]
		isVictory := uint32(player.Team) == e.body.SelectorA
		baseReward := e.playerLevels[userID] * 10
		dnaReward := baseReward
		if len(e.players) == 4 && isVictory {
			dnaReward += dnaReward / 2
		}
		if !isVictory {
			dnaReward /= 2
		}
		if len(e.players) == 2 {
			dnaReward /= 2
		}
		if e.elapsed < 3*time.Minute {
			dnaReward /= 2
		}
		if baseReward > 0 {
			dnaReward = max(1, dnaReward)
		}
		for index, participant := range e.body.Player {
			if participant.PlayerID == userID {
				e.body.DNAAmount[index] = dnaReward
				break
			}
		}
		e.statEntries = append(e.statEntries, arenaStatEntry{userID: userID, result: sporenet.PVPMatchResult{
			MatchID: e.matchID, CompletedAtUnix: completedAt.Unix(),
			DNAReward: dnaReward,
			IsVictory: isVictory,
			IsRanked:  e.isRanked, RoundWins: wins, RoundLosses: losses,
			PlayTimeSecond: uint64(max(int32(0), e.body.RoundTimeSeconds)),
			PlayerKills:    uint64(player.Kills), Deaths: uint64(player.Deaths),
			DamageDealt: float64(player.DamageDealt), DamageTaken: float64(player.DamageTaken),
			DamageMaximum: float64(maximums.damage), Healing: float64(player.HealingDealt),
			HealingReceived: float64(player.HealingReceived), HealingMaximum: float64(maximums.healing),
		}})
	}
	slices.SortFunc(e.statEntries, func(first, second arenaStatEntry) int {
		return cmp.Compare(first.userID, second.userID)
	})
}

// Save a detached completed-match projection outside the gameplay lock. The
// durable receipt makes replaying the whole batch safe after a partial failure.
func (e *gameplaySessionRegistry) saveArenaStats(ctx context.Context, gameID uint32) error {
	if ctx == nil {
		return errors.New("Arena statistics context unavailable")
	}
	if !e.beginRequest() {
		return errors.New("Arena statistics registry closed")
	}
	defer e.endRequest()
	err := e.persistArenaStats(ctx, gameID)
	if err != nil {
		return fmt.Errorf("arenaPersist: %w", err)
	}
	return nil
}

// Cleanup calls this after closing request admission, before discarding matches.
func (e *gameplaySessionRegistry) persistArenaStats(ctx context.Context, gameID uint32) error {
	e.mutex.RLock()
	match := e.arenaMatches[gameID]
	if match == nil || !match.result.isCompleted || match.result.isStatsSaved {
		e.mutex.RUnlock()
		return nil
	}
	entries := append([]arenaStatEntry(nil), match.result.statEntries...)
	recorder := e.playerStatRecorder
	e.mutex.RUnlock()
	if len(entries) != 0 && recorder == nil {
		return errors.New("Arena statistics recorder unavailable")
	}
	for _, entry := range entries {
		err := recorder.RecordPVPMatch(ctx, entry.userID, entry.result)
		if err != nil {
			return fmt.Errorf("arenaPlayerSave[%d]: %w", entry.userID, err)
		}
	}
	e.mutex.Lock()
	if e.arenaMatches[gameID] == match {
		match.result.isStatsSaved = true
		cancel := e.retireArenaResultLocked(gameID)
		if cancel != nil {
			cancel()
		}
	}
	e.mutex.Unlock()
	if e.logger != nil {
		e.logger.Printf("Arena match statistics saved game=%d match=%s players=%d", gameID, match.result.matchID, len(entries))
		for _, entry := range entries {
			e.logger.Printf(
				"Arena player statistics saved game=%d match=%s user=%d is_victory=%t round_wins=%d round_losses=%d",
				gameID, entry.result.MatchID, entry.userID, entry.result.IsVictory,
				entry.result.RoundWins, entry.result.RoundLosses,
			)
		}
	}
	return nil
}

func (e *gameplaySessionRegistry) flushArenaStats() {
	e.mutex.RLock()
	gameIDs := make([]uint32, 0, len(e.arenaMatches))
	for gameID, match := range e.arenaMatches {
		if match.result.isCompleted && !match.result.isStatsSaved {
			gameIDs = append(gameIDs, gameID)
		}
	}
	e.mutex.RUnlock()
	for _, gameID := range gameIDs {
		err := e.persistArenaStats(context.Background(), gameID)
		if err != nil && e.logger != nil {
			e.logger.Printf("Arena completed-match statistics shutdown save failed game=%d: %v", gameID, err)
		}
	}
}
