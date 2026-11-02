//go:build scenario

package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/scenario"
)

type debugWorldStateProvider interface {
	ScenarioWorldState(context.Context, int64, uint32) (scenario.WorldState, error)
}

// DebugWorldState observes only an already authorized owned identity. Admission
// and current gameplay transport matching remain owned by the gameplay registry.
func (e *Server) DebugWorldState(ctx context.Context, userID int64, gameID uint32) (scenario.WorldState, error) {
	if e == nil || ctx == nil || userID <= 0 || gameID == 0 {
		return scenario.WorldState{}, errors.New("world state requires an owned server, context and admitted user/game")
	}
	provider, isSupported := e.bugContext.(debugWorldStateProvider)
	if !isSupported {
		return scenario.WorldState{UnavailableReason: "world observer is unavailable"}, nil
	}
	result, err := provider.ScenarioWorldState(ctx, userID, gameID)
	if err != nil {
		return result, fmt.Errorf("worldObserve: %w", err)
	}
	return result, nil
}
