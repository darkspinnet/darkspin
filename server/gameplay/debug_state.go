//go:build scenario

package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/scenario"
)

type scenarioGameplayStateProvider interface {
	ScenarioGameplayState(context.Context, int64, uint32) (scenario.GameplayState, error)
}

func (e Lifecycle) ScenarioGameplayState(ctx context.Context, userID int64, gameID uint32) (scenario.GameplayState, error) {
	if ctx == nil || userID <= 0 {
		return scenario.GameplayState{}, errors.New("gameplay state requires context and a positive owned user")
	}
	provider, isSupported := e.syncSnapshot.(scenarioGameplayStateProvider)
	if !isSupported {
		return scenario.GameplayState{UnavailableReason: "gameplay lifecycle observer is unavailable"}, nil
	}
	state, err := provider.ScenarioGameplayState(ctx, userID, gameID)
	if err != nil {
		return state, fmt.Errorf("gameplayStateProvider: %w", err)
	}
	return state, nil
}

// ScenarioGameplayState samples only lifecycle scalars while the registry owns
// its read lock. In particular it never queries squad characters, hero resources,
// director objects, or any full diagnostic context during a mission handshake.
func (e *gameplaySessionRegistry) ScenarioGameplayState(ctx context.Context, userID int64, gameID uint32) (scenario.GameplayState, error) {
	if ctx == nil || userID <= 0 {
		return scenario.GameplayState{}, errors.New("gameplay state requires context and a positive owned user")
	}
	result := scenario.GameplayState{}
	err := ctx.Err()
	if err != nil {
		return result, fmt.Errorf("gameplayStateContext: %w", err)
	}
	if e == nil {
		result.UnavailableReason = "gameplay registry is unavailable"
		return result, nil
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !e.mutex.TryRLock() {
		select {
		case <-ctx.Done():
			return result, fmt.Errorf("gameplayStateLock: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	defer e.mutex.RUnlock()
	if e.isClosed {
		result.UnavailableReason = "gameplay registry is closed"
		return result, nil
	}
	matchedCount := 0
	for endpoint, peerSession := range e.sessions {
		binding := peerSession.binding
		if binding.UserID != uint64(userID) || (gameID != 0 && binding.GameID != gameID) {
			continue
		}
		matchedCount++
		if matchedCount > 1 {
			return scenario.GameplayState{UnavailableReason: "multiple live gameplay bindings match the owned user and game"}, nil
		}
		memberKey := gameplaySessionMemberKey(peerSession)
		if binding.GameID == 0 || peerSession.generation == 0 || peerSession.transportGeneration == 0 || e.memberEndpoints[memberKey] != endpoint || e.memberTransports[memberKey] != peerSession.transportGeneration {
			return scenario.GameplayState{UnavailableReason: "gameplay binding is not the current admitted transport"}, nil
		}
		result.IsGameplayActive = true
		result.GameID = binding.GameID
		result.SessionStage = bugSessionStage(peerSession.stage)
		result.MissionLabel = bugMissionLabel(binding)
		result.MissionAsset = binding.Level
		result.MissionDifficulty = binding.Difficulty
		result.IsDungeonSetupFinal = peerSession.dungeonSetup.IsCommitted()
		result.IsArena = binding.Mode == game.ModeArena
		result.IsArenaSquadAccepted = peerSession.isArenaSquadAccepted
		match := e.arenaMatches[binding.GameID]
		if result.IsArena && match != nil {
			result.IsArenaCombatActive = match.isCombatActive
		}
	}
	err = ctx.Err()
	if err != nil {
		return scenario.GameplayState{}, fmt.Errorf("gameplayStateScan: %w", err)
	}
	result.IsAvailable = true
	return result, nil
}
