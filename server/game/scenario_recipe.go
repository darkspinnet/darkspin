//go:build scenario

package game

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/darkspinnet/darkspin/server/scenario"
)

type scenarioRecipeState struct {
	recipesByUser map[int64]scenario.FocusedRecipe
}
type scenarioRecipeBinding struct {
	focusedRecipe            scenario.FocusedRecipe
	focusedParticipantIDs    []uint64
	isFocusedCohortValidated bool
}

func (e *Manager) ConfigureFocusedRecipe(ctx context.Context, req scenario.FocusedRecipeRequest) (scenario.FocusedRecipe, error) {
	if ctx == nil || e == nil || req.UserID <= 0 || req.RunSeed == 0 || req.RunID == "" || len(req.RunID) > 80 {
		return scenario.FocusedRecipe{}, errors.New("focused recipe requires context, owned user/run and a nonzero server run seed")
	}
	for _, character := range req.RunID {
		if character != '-' && character != '_' && !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') {
			return scenario.FocusedRecipe{}, errors.New("focused recipe run identity is invalid")
		}
	}
	err := ctx.Err()
	if err != nil {
		return scenario.FocusedRecipe{}, fmt.Errorf("recipeContext: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	configuredRecipe, isConfigured := e.recipesByUser[req.UserID]
	if isConfigured {
		return scenario.FocusedRecipe{}, fmt.Errorf("focused recipe is immutable once configured for run %q", configuredRecipe.RunID)
	}
	parityRequest, isParity := e.requests[req.UserID]
	if isParity {
		return scenario.FocusedRecipe{}, fmt.Errorf("focused recipe cannot replace parity run %q", parityRequest.RunID)
	}
	for _, instance := range e.games {
		if instance.HasPlayer(req.UserID) {
			return scenario.FocusedRecipe{}, errors.New("focused recipe must be configured before game admission")
		}
	}
	err = ctx.Err()
	if err != nil {
		return scenario.FocusedRecipe{}, fmt.Errorf("recipeCommitContext: %w", err)
	}
	recipe := scenario.FocusedRecipe{RunID: req.RunID, UserID: req.UserID, RunSeed: req.RunSeed, IsAmbientPopulationSuppressed: req.IsAmbientPopulationSuppressed}
	if e.recipesByUser == nil {
		e.recipesByUser = make(map[int64]scenario.FocusedRecipe)
	}
	e.recipesByUser[req.UserID] = recipe
	return recipe, nil
}

func (e *Manager) attachFocusedRecipe(binding *GameplayBinding) error {
	e.mu.RLock()
	instance := e.games[binding.GameID]
	recipesByUser := make(map[int64]scenario.FocusedRecipe, len(e.recipesByUser))
	for userID, recipe := range e.recipesByUser {
		recipesByUser[userID] = recipe
	}
	parityUsers := make(map[int64]bool, len(e.requests))
	for userID := range e.requests {
		parityUsers[userID] = true
	}
	e.mu.RUnlock()
	if instance == nil {
		configuredRecipe, isConfigured := recipesByUser[int64(binding.UserID)]
		if isConfigured {
			return fmt.Errorf("focused recipe game instance is unavailable for run %q", configuredRecipe.RunID)
		}
		return nil
	}
	// Recipes are immutable once configured. Copy them before taking the
	// instance lock, preserving the manager/instance lock ownership boundary.
	recipe, participantIDs, err := instance.validateFocusedCohort(*binding, recipesByUser, parityUsers)
	if err != nil {
		return fmt.Errorf("recipeCohort: %w", err)
	}
	if recipe.RunID == "" {
		return nil
	}
	// IsReplay is the account's HasBeatenThisLevel predicate. A fresh profile
	// whose progression is 1 legitimately sets it for 1-1; it does not mean this
	// process or zone was restored. Preserve that ordinary gameplay decision.
	binding.focusedRecipe = recipe
	binding.focusedRecipe.UserID = int64(binding.UserID)
	binding.focusedParticipantIDs = participantIDs
	binding.isFocusedCohortValidated = true
	binding.RunSeed = recipe.RunSeed
	return nil
}

func (e *Instance) validateFocusedCohort(binding GameplayBinding, recipesByUser map[int64]scenario.FocusedRecipe, parityUsers map[int64]bool) (scenario.FocusedRecipe, []uint64, error) {
	if e == nil {
		return scenario.FocusedRecipe{}, nil, errors.New("focused recipe game instance is unavailable")
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	recipe, isConfigured := recipesByUser[int64(binding.UserID)]
	if !isConfigured {
		for userID := range e.players {
			candidate, isCandidate := recipesByUser[userID]
			if isCandidate {
				recipe = candidate
				break
			}
		}
	}
	if recipe.RunID == "" {
		return scenario.FocusedRecipe{}, nil, nil
	}
	if e.ID != binding.GameID || e.Info.Mode != ModeChain || binding.Mode != ModeChain || e.Info.IsWarped || e.isCheckpointRestore || binding.IsWarped || binding.IsCheckpointRestore {
		return scenario.FocusedRecipe{}, nil, errors.New("focused recipe game identity is not a fresh ordinary campaign instance")
	}
	if binding.ParticipantCount == 0 || binding.ParticipantCount > MaxGamePlayers || len(e.players) != int(binding.ParticipantCount) {
		return scenario.FocusedRecipe{}, nil, fmt.Errorf("focused recipe actual roster does not match frozen cardinality: players=%d frozen=%d", len(e.players), binding.ParticipantCount)
	}
	if e.Info.ExpectedPlayerCount != 0 && e.Info.ExpectedPlayerCount != binding.ParticipantCount {
		return scenario.FocusedRecipe{}, nil, errors.New("focused recipe expected cardinality changed")
	}
	if binding.ParticipantCount > 1 && (!e.isAdmissionReserved || e.Info.ExpectedPlayerCount != binding.ParticipantCount) {
		return scenario.FocusedRecipe{}, nil, errors.New("shared focused recipe requires the complete reserved frozen roster")
	}
	user, isJoined := e.players[int64(binding.UserID)]
	if !isJoined || user == nil || user.Account.ID != int64(binding.UserID) {
		return scenario.FocusedRecipe{}, nil, errors.New("focused recipe actor is not an actual frozen member")
	}
	slot, isSlot := e.slots[int64(binding.UserID)]
	if !isSlot || slot != binding.Slot || binding.PlayerMask != e.playerMaskLocked() {
		return scenario.FocusedRecipe{}, nil, errors.New("focused recipe binding does not match actual frozen slots")
	}
	participantIDs := make([]uint64, 0, len(e.players))
	for userID, participant := range e.players {
		if participant == nil || userID <= 0 || participant.Account.ID != userID {
			return scenario.FocusedRecipe{}, nil, errors.New("focused recipe contains an invalid actual participant")
		}
		participantRecipe, isParticipantConfigured := recipesByUser[userID]
		if !isParticipantConfigured || participantRecipe.UserID != userID || participantRecipe.RunID != recipe.RunID || participantRecipe.RunSeed != recipe.RunSeed || participantRecipe.IsAmbientPopulationSuppressed != recipe.IsAmbientPopulationSuppressed {
			return scenario.FocusedRecipe{}, nil, errors.New("every frozen focused participant must preconfigure the same run, seed and ambient population policy")
		}
		if parityUsers[userID] {
			return scenario.FocusedRecipe{}, nil, errors.New("focused cohort cannot contain a parity scenario participant")
		}
		participantIDs = append(participantIDs, uint64(userID))
	}
	sort.Slice(participantIDs, func(left, right int) bool { return participantIDs[left] < participantIDs[right] })
	return recipe, participantIDs, nil
}

func (e GameplayBinding) FocusedRecipeInputs() (runID string, isAmbientPopulationSuppressed bool) {
	return e.focusedRecipe.RunID, e.focusedRecipe.IsAmbientPopulationSuppressed
}

// FocusedRecipeCohort exposes only membership already validated against the
// actual reserved game roster. Consumers must still match current zone peers.
func (e GameplayBinding) FocusedRecipeCohort(runID string) ([]uint64, error) {
	if !e.isFocusedCohortValidated || runID == "" || e.focusedRecipe.RunID != runID || e.focusedRecipe.UserID != int64(e.UserID) || e.focusedRecipe.RunSeed != e.RunSeed || len(e.focusedParticipantIDs) != int(e.ParticipantCount) {
		return nil, errors.New("focused binding does not retain a validated run and frozen cohort")
	}
	return append([]uint64(nil), e.focusedParticipantIDs...), nil
}
