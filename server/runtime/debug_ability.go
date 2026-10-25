//go:build scenario

package runtime

import (
	"context"
	"errors"
	"fmt"
	"github.com/darkspinnet/darkspin/server/scenario"
)

type debugAbilityStateProvider interface {
	ScenarioAbilityState(context.Context, int64, uint32) (scenario.AbilityState, error)
}

func (e *Server) DebugAbilityState(ctx context.Context, userID int64, gameID uint32) (scenario.AbilityState, error) {
	if e == nil || ctx == nil || userID <= 0 || gameID == 0 {
		return scenario.AbilityState{}, errors.New("ability state requires owned server, context and admitted user/game")
	}
	provider, isSupported := e.bugContext.(debugAbilityStateProvider)
	if !isSupported {
		return scenario.AbilityState{UnavailableReason: "ability observer is unavailable"}, nil
	}
	result, err := provider.ScenarioAbilityState(ctx, userID, gameID)
	if err != nil {
		return result, fmt.Errorf("abilityObserve: %w", err)
	}
	return result, nil
}
