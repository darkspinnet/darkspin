package game

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/sim"
)

// CampaignDestination separates the selected threat from the map played at it.
// The unlocked-map intersection and uniform unique-map draw are server policy.
type CampaignDestination struct {
	Level              string
	IsReplayRandomized bool
}

// SelectCampaignDestination runs after the complete party has been admitted,
// before publishing the shared destination. Restores retain their saved map.
func (e *Manager) SelectCampaignDestination(instance *Instance) error {
	if e == nil || instance == nil {
		return errors.New("campaign destination unavailable")
	}
	if instance.Info.Mode != ModeChain || instance.Info.IsWarped || instance.IsCheckpointRestore() {
		return nil
	}
	difficulty, err := resolveGameplayDifficulty(instance.Info)
	if err != nil {
		return fmt.Errorf("destinationDifficulty: %w", err)
	}
	players := instance.Players()
	completedStages := make([]uint32, 0, len(players))
	for _, player := range players {
		if player == nil {
			return errors.New("campaign destination member unavailable")
		}
		completedStages = append(completedStages, player.PresenceSnapshot().ChainProgression)
	}
	runSeed, err := e.campaignDestinationRunSeed(instance)
	if err != nil {
		return fmt.Errorf("destinationSeed: %w", err)
	}
	destination, err := e.campaignDestination(difficulty, runSeed, completedStages)
	if err != nil {
		return fmt.Errorf("destinationSelect: %w", err)
	}
	instance.mu.Lock()
	instance.RunSeed = runSeed
	instance.Info.Level = destination.Level
	instance.Info.IsReplayRandomized = destination.IsReplayRandomized
	instance.campaignDestinations = map[uint32]CampaignDestination{difficulty: destination}
	instance.mu.Unlock()
	return nil
}

func (e *Manager) campaignDestination(
	difficulty uint32, runSeed uint64, completedStages []uint32,
) (CampaignDestination, error) {
	destination, err := e.campaignDestinationAfter(difficulty, runSeed, completedStages, "")
	if err != nil {
		return CampaignDestination{}, fmt.Errorf("destinationInitial: %w", err)
	}
	return destination, nil
}

func (e *Manager) campaignDestinationAfter(
	difficulty uint32, runSeed uint64, completedStages []uint32, previousLevel string,
) (CampaignDestination, error) {
	level, isFound := e.ChainLevelForSelection(difficulty)
	if !isFound || len(completedStages) == 0 {
		return CampaignDestination{}, errors.New("campaign destination selection unavailable")
	}
	completed := completedStages[0]
	for _, progress := range completedStages[1:] {
		completed = min(completed, progress)
	}
	if uint64(difficulty) > uint64(completed)+1 {
		return CampaignDestination{}, errors.New("campaign difficulty locked for party")
	}
	if difficulty > completed {
		return CampaignDestination{Level: level}, nil
	}
	if runSeed == 0 {
		return CampaignDestination{}, errors.New("campaign destination seed unavailable")
	}
	levels := e.unlockedCampaignLevels(completed)
	// Replay policy avoids an immediate repeat without changing locked first
	// runs, retained offers, or the only available destination.
	if previousLevel != "" && len(levels) > 1 {
		candidates := make([]string, 0, len(levels))
		for _, candidate := range levels {
			if !strings.EqualFold(candidate, previousLevel) {
				candidates = append(candidates, candidate)
			}
		}
		levels = candidates
	}
	if len(levels) == 0 {
		return CampaignDestination{}, errors.New("campaign replay maps unavailable")
	}
	// Separate the map draw from creature and loot streams. The same run and
	// threat must produce the same destination for every party member.
	seed := runSeed ^ uint64(difficulty)*0x9e3779b97f4a7c15 ^ 0x7265706c6179
	seed ^= seed >> 30
	seed *= 0xbf58476d1ce4e5b9
	seed ^= seed >> 27
	seed *= 0x94d049bb133111eb
	seed ^= seed >> 31
	random := sim.NewSimulatorRandom(uint32(seed) ^ uint32(seed>>32))
	index, err := random.Index(uint32(len(levels)))
	if err != nil {
		return CampaignDestination{}, fmt.Errorf("destinationDraw: %w", err)
	}
	return CampaignDestination{Level: levels[index], IsReplayRandomized: true}, nil
}

func (e *Manager) unlockedCampaignLevels(completed uint32) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.chainLevels) == 0 {
		return []string{InitialChainLevel}
	}
	count := min(uint64(len(e.chainLevels)), uint64(completed)+1)
	levels := make([]string, 0, count)
	seenLevels := make(map[string]struct{}, count)
	for _, reference := range e.chainLevels[:count] {
		level := strings.TrimSuffix(reference, ".Level")
		key := strings.ToLower(level)
		if key == "" {
			continue
		}
		if _, isSeen := seenLevels[key]; isSeen {
			continue
		}
		seenLevels[key] = struct{}{}
		levels = append(levels, level)
	}
	return levels
}

// ValidateCampaignAdmission never rerolls an already published destination.
func (e *Manager) ValidateCampaignAdmission(instance *Instance, completed uint32) error {
	if e == nil || instance == nil {
		return errors.New("campaign admission unavailable")
	}
	instance.mu.RLock()
	mode, isWarped := instance.Info.Mode, instance.Info.IsWarped
	isReplayRandomized, level := instance.Info.IsReplayRandomized, instance.Info.Level
	difficulty, err := resolveGameplayDifficulty(instance.Info)
	pendingDestinations := make(map[uint32]CampaignDestination)
	for stage, destination := range instance.campaignDestinations {
		if stage > difficulty && destination.IsReplayRandomized {
			pendingDestinations[stage] = destination
		}
	}
	instance.mu.RUnlock()
	if mode != ModeChain || isWarped {
		return nil
	}
	if err != nil {
		return fmt.Errorf("admissionDifficulty: %w", err)
	}
	if uint64(difficulty) > uint64(completed)+1 {
		return errors.New("campaign difficulty locked")
	}
	if !isReplayRandomized && len(pendingDestinations) == 0 {
		return nil
	}
	unlockedLevels := make(map[string]struct{})
	for _, candidate := range e.unlockedCampaignLevels(completed) {
		unlockedLevels[strings.ToLower(candidate)] = struct{}{}
	}
	if isReplayRandomized {
		if difficulty > completed {
			return errors.New("campaign replay requires completed difficulty")
		}
		if _, isUnlocked := unlockedLevels[strings.ToLower(level)]; !isUnlocked {
			return errors.New("campaign replay destination locked")
		}
	}
	for stage, destination := range pendingDestinations {
		if stage > completed {
			return errors.New("campaign replay offer difficulty locked")
		}
		if _, isUnlocked := unlockedLevels[strings.ToLower(destination.Level)]; !isUnlocked {
			return errors.New("campaign replay offer destination locked")
		}
	}
	return nil
}

// ResolveCampaignDestination prepares the next offer without changing the
// current game. The result snapshot retains this choice until Continue.
func (e *GameplayJoin) ResolveCampaignDestination(
	ctx context.Context, binding GameplayBinding,
) (CampaignDestination, error) {
	if binding.Difficulty < 2 {
		return CampaignDestination{}, errors.New("next campaign difficulty invalid")
	}
	completedStages, err := e.CampaignPartyCompletedStages(ctx, binding)
	if err != nil {
		return CampaignDestination{}, fmt.Errorf("destinationParty: %w", err)
	}
	instance := e.gameManager.Game(binding.GameID)
	if instance == nil {
		return CampaignDestination{}, errors.New("campaign destination game unavailable")
	}
	instance.mu.RLock()
	retained, isRetained := instance.campaignDestinations[binding.Difficulty]
	previousLevel := instance.Info.Level
	instance.mu.RUnlock()
	if isRetained {
		return retained, nil
	}
	// A completed mission may be offered before its progression transaction.
	// Account for that completion when validating the next unlocked threat.
	for index := range completedStages {
		completedStages[index] = max(completedStages[index], binding.Difficulty-1)
	}
	destination, err := e.gameManager.campaignDestinationAfter(
		binding.Difficulty, binding.RunSeed, completedStages, previousLevel,
	)
	if err != nil {
		return CampaignDestination{}, fmt.Errorf("destinationResolve: %w", err)
	}
	instance.mu.Lock()
	defer instance.mu.Unlock()
	if retained, isRetained := instance.campaignDestinations[binding.Difficulty]; isRetained {
		return retained, nil
	}
	if instance.campaignDestinations == nil {
		instance.campaignDestinations = make(map[uint32]CampaignDestination)
	}
	instance.campaignDestinations[binding.Difficulty] = destination
	return destination, nil
}

// RetainCampaignDestination publishes an accepted Continue choice for rejoin.
// Repeated party acknowledgements of the same choice are idempotent.
func (e *GameplayJoin) RetainCampaignDestination(binding GameplayBinding) error {
	if e == nil || e.gameManager == nil || e.userFinder == nil {
		return errors.New("campaign retention unavailable")
	}
	user := e.userFinder.UserByID(int64(binding.UserID))
	instance := e.gameManager.Game(binding.GameID)
	if user == nil || instance == nil || user.CurrentGameID() != binding.GameID ||
		!instance.HasPlayer(int64(binding.UserID)) {
		return errors.New("campaign retention member unavailable")
	}
	instance.mu.Lock()
	defer instance.mu.Unlock()
	if instance.RunSeed != binding.RunSeed || instance.Info.Mode != ModeChain {
		return errors.New("campaign retention run mismatch")
	}
	difficulty, err := resolveGameplayDifficulty(instance.Info)
	if err != nil {
		return fmt.Errorf("retentionDifficulty: %w", err)
	}
	if binding.Difficulty == difficulty {
		if !strings.EqualFold(instance.Info.Level, binding.Level) ||
			instance.Info.IsReplayRandomized != binding.IsReplayRandomized {
			return errors.New("campaign retention destination mismatch")
		}
		return nil
	}
	if binding.Difficulty != difficulty+1 || binding.Level == "" {
		return errors.New("campaign retention stage mismatch")
	}
	instance.Info.Level = binding.Level
	instance.Info.IsReplayRandomized = binding.IsReplayRandomized
	instance.Info.Attributes["SelectedDifficulty"] = fmt.Sprintf("%d", binding.Difficulty)
	return nil
}
