//go:build scenario

package game

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ScenarioMapRequest binds explicit native map inputs to one isolated account.
// HeroNouns are ordered template noun identities, not persistent creature IDs.
type ScenarioMapRequest struct {
	UserID      int64
	RunID       string
	Level       string
	Occurrence  uint32
	Difficulty  uint32
	MapSeed     uint32
	PrepareMask uint32
	SquadID     uint32
	HeroNouns   [3]uint32
}

type scenarioMapState struct {
	requests            map[int64]ScenarioMapRequest
	continueGamesByUser map[int64]*Instance
}

type scenarioMapBinding struct {
	req          ScenarioMapRequest
	isConfigured bool
}

// ConfigureScenario installs immutable inputs before ordinary client joins.
// It neither creates a game nor changes player membership or readiness.
func (e *Manager) ConfigureScenario(ctx context.Context, req ScenarioMapRequest) error {
	if ctx == nil {
		return errors.New("scenario map context unavailable")
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("mapContext: %w", err)
	}
	if e == nil {
		return errors.New("scenario game manager unavailable")
	}
	err = req.validate()
	if err != nil {
		return fmt.Errorf("mapRequest: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if int(req.Occurrence) > len(e.chainLevels) ||
		!strings.EqualFold(strings.TrimSuffix(e.chainLevels[req.Occurrence-1], ".Level"), req.Level) {
		return errors.New("scenario authored campaign occurrence mismatch")
	}
	configuredReq, isFound := e.requests[req.UserID]
	if isFound {
		return fmt.Errorf("scenario user already configured for run %q", configuredReq.RunID)
	}
	for _, configured := range e.requests {
		if configured.RunID == req.RunID {
			return errors.New("scenario run already configured")
		}
	}
	for _, instance := range e.games {
		if instance.HasPlayer(req.UserID) {
			return errors.New("scenario user already joined")
		}
	}
	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("mapCommitContext: %w", err)
	}
	if e.requests == nil {
		e.requests = make(map[int64]ScenarioMapRequest)
	}
	e.requests[req.UserID] = req
	return nil
}

func (e ScenarioMapRequest) validate() error {
	if e.UserID <= 0 || e.SquadID == 0 || len(e.RunID) == 0 || len(e.RunID) > 80 {
		return errors.New("scenario identity unavailable")
	}
	for _, character := range e.RunID {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return errors.New("scenario run identity invalid")
	}
	if e.Level != "zelems_3" || e.Occurrence != 2 || e.Difficulty != 2 {
		return errors.New("scenario mission must be normal 1-2")
	}
	if e.MapSeed == 0 || e.MapSeed == ^uint32(0) || e.PrepareMask != 0 {
		return errors.New("scenario map seed or prepare mask invalid")
	}
	for index, noun := range e.HeroNouns {
		if noun == 0 {
			return fmt.Errorf("scenario hero noun[%d] unavailable", index)
		}
		for preceding := 0; preceding < index; preceding++ {
			if e.HeroNouns[preceding] == noun {
				return errors.New("scenario hero nouns must be distinct")
			}
		}
	}
	return nil
}

func (e *Manager) attachScenarioMap(binding *GameplayBinding) error {
	if e == nil || binding == nil {
		return errors.New("scenario binding unavailable")
	}
	e.mu.RLock()
	req, isConfigured := e.requests[int64(binding.UserID)]
	e.mu.RUnlock()
	if !isConfigured {
		binding.scenarioMapBinding = scenarioMapBinding{}
		return nil
	}
	err := req.validateBinding(*binding)
	if err != nil {
		return fmt.Errorf("mapBinding: %w", err)
	}
	binding.scenarioMapBinding = scenarioMapBinding{req: req, isConfigured: true}
	return nil
}

func (e ScenarioMapRequest) validateBinding(binding GameplayBinding) error {
	if binding.UserID != uint64(e.UserID) || binding.Mode != ModeChain ||
		binding.IsWarped || binding.IsCheckpointRestore || binding.IsReplay {
		return errors.New("scenario requires fresh ordinary campaign binding")
	}
	if binding.Level != e.Level || binding.ChainLevelIndex != e.Occurrence ||
		binding.Difficulty != e.Difficulty || binding.ChainProgression != 1 {
		return errors.New("scenario mission binding mismatch")
	}
	if binding.ParticipantCount != 1 || binding.PlayerMask != 1 || binding.Slot != 0 {
		return errors.New("scenario requires solo campaign binding")
	}
	if binding.SquadID != e.SquadID {
		return errors.New("scenario squad binding mismatch")
	}
	for index, creature := range binding.Creatures {
		if creature.ID == 0 || creature.Noun != e.HeroNouns[index] {
			return fmt.Errorf("scenario hero binding[%d] mismatch", index)
		}
	}
	return nil
}

// ScenarioMapInputs revalidates retained inputs against the current binding.
// Ordinary mission/squad transitions cannot accidentally reuse a scenario seed.
func (e GameplayBinding) ScenarioMapInputs() (
	mapSeed uint32, prepareMask uint32, isConfigured bool, err error,
) {
	if !e.isConfigured {
		return 0, 0, false, nil
	}
	err = e.req.validateBinding(e)
	if err != nil {
		return 0, 0, false, fmt.Errorf("mapInputs: %w", err)
	}
	return e.req.MapSeed, e.req.PrepareMask, true, nil
}
