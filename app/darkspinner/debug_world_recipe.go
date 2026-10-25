//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/scenario"
)

type debugWorldRecipeRequest struct {
	ClientID                      string `json:"client_id"`
	RunSeed                       uint64 `json:"run_seed"`
	IsAmbientPopulationSuppressed bool   `json:"is_ambient_population_suppressed"`
}

type debugWorldRecipe struct {
	ClientID                      string `json:"client_id"`
	InstanceID                    string `json:"instance_id"`
	PID                           uint32 `json:"pid"`
	UserID                        int64  `json:"user_id"`
	RunSeed                       uint64 `json:"run_seed"`
	IsAmbientPopulationSuppressed bool   `json:"is_ambient_population_suppressed"`
	Source                        string `json:"source"`
}

func configureDebugWorld(ctx context.Context, host *debugHost, clientManager *debugClientManager, payload json.RawMessage) (debugWorldRecipe, error) {
	result := debugWorldRecipe{Source: "server_focused_recipe"}
	var req debugWorldRecipeRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return result, fmt.Errorf("recipeRequest: %w", err)
	}
	if req.RunSeed == 0 || req.RunSeed > 9007199254740991 {
		return result, errors.New("run seed must be a positive exactly representable Lua integer")
	}
	owner, err := observeDebugWorldOwner(ctx, host, clientManager, req.ClientID)
	if err != nil {
		return result, fmt.Errorf("recipeOwner: %w", err)
	}
	if owner.profile.GameID != 0 || owner.profile.IsGameplayActive {
		return result, errors.New("configure a focused recipe after accepted login and before mission admission")
	}
	result.ClientID = owner.client.ClientID
	result.InstanceID = owner.client.InstanceID
	result.PID = owner.client.ProcessID
	result.UserID = owner.client.UserID
	err = recheckDebugWorldOwner(ctx, host, clientManager, owner)
	if err != nil {
		return result, fmt.Errorf("recipePrecheck: %w", err)
	}
	recipe, err := host.server.DebugConfigureRecipe(ctx, scenario.FocusedRecipeRequest{RunID: host.runID, UserID: owner.client.UserID, RunSeed: req.RunSeed, IsAmbientPopulationSuppressed: req.IsAmbientPopulationSuppressed})
	if err != nil {
		return result, fmt.Errorf("recipeConfigure: %w", err)
	}
	result.RunSeed = recipe.RunSeed
	result.IsAmbientPopulationSuppressed = recipe.IsAmbientPopulationSuppressed
	err = recheckDebugWorldOwner(ctx, host, clientManager, owner)
	if err != nil {
		return result, fmt.Errorf("recipePostcheck: %w", err)
	}
	return result, nil
}
