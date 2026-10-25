//go:build scenario

package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/scenario"
)

type debugSpawnProvider interface {
	ScenarioSpawnNPC(context.Context, scenario.SpawnRequest) (scenario.SpawnReceipt, error)
	ScenarioSpawnState(context.Context, string, int64, string) (scenario.SpawnReceipt, error)
}

// The supervisor authorizes ownership before this feature boundary. Recipes
// remain immutable and cannot be installed after the account enters a game.
func (e *Server) DebugConfigureRecipe(ctx context.Context, req scenario.FocusedRecipeRequest) (scenario.FocusedRecipe, error) {
	if e == nil || e.gameManager == nil || e.userManager == nil || ctx == nil {
		return scenario.FocusedRecipe{}, errors.New("focused recipe service unavailable")
	}
	user := e.userManager.UserByID(req.UserID)
	if user == nil || user.CurrentGameID() != 0 {
		return scenario.FocusedRecipe{}, errors.New("focused recipe requires an active owned user before game admission")
	}
	recipe, err := e.gameManager.ConfigureFocusedRecipe(ctx, req)
	if err != nil {
		return recipe, fmt.Errorf("recipeConfigure: %w", err)
	}
	return recipe, nil
}

func (e *Server) DebugSpawnNPC(ctx context.Context, req scenario.SpawnRequest) (scenario.SpawnReceipt, error) {
	if e == nil || ctx == nil {
		return scenario.SpawnReceipt{}, errors.New("focused spawn service unavailable")
	}
	provider, isSupported := e.bugContext.(debugSpawnProvider)
	if !isSupported {
		return scenario.SpawnReceipt{}, errors.New("focused spawn provider unavailable")
	}
	receipt, err := provider.ScenarioSpawnNPC(ctx, req)
	if err != nil {
		return receipt, fmt.Errorf("spawnRequest: %w", err)
	}
	return receipt, nil
}

func (e *Server) DebugSpawnState(ctx context.Context, runID string, userID int64, requestID string) (scenario.SpawnReceipt, error) {
	if e == nil || ctx == nil {
		return scenario.SpawnReceipt{}, errors.New("focused spawn state service unavailable")
	}
	provider, isSupported := e.bugContext.(debugSpawnProvider)
	if !isSupported {
		return scenario.SpawnReceipt{}, errors.New("focused spawn provider unavailable")
	}
	receipt, err := provider.ScenarioSpawnState(ctx, runID, userID, requestID)
	if err != nil {
		return receipt, fmt.Errorf("spawnState: %w", err)
	}
	return receipt, nil
}
