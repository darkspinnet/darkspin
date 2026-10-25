//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type debugMissionSelectResult struct {
	Actions           []any `json:"actions"`
	Selection         any   `json:"selection"`
	IsAlreadySelected bool  `json:"is_already_selected"`
}

type debugMissionSelector struct {
	host          *debugHost
	clientManager *debugClientManager
	actor         debugMissionActionActor
	result        debugMissionSelectResult
}

// Select navigates only the first campaign mode using guarded authored actions.
// An attempted native action is recorded once and is never retried.
func selectDebugMission(ctx context.Context, host *debugHost, clientManager *debugClientManager, payload json.RawMessage) (debugMissionSelectResult, error) {
	selector := debugMissionSelector{host: host, clientManager: clientManager, result: debugMissionSelectResult{Actions: make([]any, 0)}}
	if ctx == nil || host == nil || clientManager == nil {
		return selector.result, errors.New("mission selection requires context and an owned host and client manager")
	}
	var req debugMapRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return selector.result, fmt.Errorf("selectRequest: %w", err)
	}
	if req.ClientID == "" || req.Difficulty < 1 || req.Difficulty > 24 || req.TimeoutMS == 0 || req.TimeoutMS > 120000 {
		return selector.result, errors.New("mission selection requires client_id, first-campaign difficulty from 1 to 24, and timeout_ms from 1 to 120000")
	}
	selectionContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	selector.actor, err = sampleDebugMissionActor(selectionContext, host, clientManager, req.ClientID)
	if err != nil {
		return selector.result, fmt.Errorf("selectActor: %w", err)
	}
	if selector.actor.profile.GameID != 0 {
		return selector.result, errors.New("mission selection requires an authenticated client outside an active game")
	}
	selection, err := selector.waitEntry(selectionContext)
	if err != nil {
		return selector.result, fmt.Errorf("selectEntry: %w", err)
	}
	if selection.State != nil && *selection.State == 1 {
		err = selector.recheck(selectionContext)
		if err != nil {
			return selector.result, fmt.Errorf("selectCampaignIdentity: %w", err)
		}
		action, actionErr := clientManager.OpenCampaign(selectionContext, req.ClientID)
		if action != nil {
			selector.result.Actions = append(selector.result.Actions, action)
		}
		if actionErr != nil {
			return selector.result, fmt.Errorf("selectCampaignAction: %w", actionErr)
		}
		if action == nil {
			return selector.result, errors.New("campaign action returned no receipt; do not replay")
		}
		selection, err = selector.waitSelection(selectionContext, req, 0)
		if err != nil {
			return selector.result, fmt.Errorf("selectCampaignWait: %w", err)
		}
	}
	if selection.Difficulty == nil || *selection.Difficulty < 1 || *selection.Difficulty > 24 {
		return selector.result, errors.New("native selection is outside the first campaign mode")
	}
	difficulty := *selection.Difficulty
	selector.result.IsAlreadySelected = difficulty == req.Difficulty && len(selector.result.Actions) == 0
	for difficulty != req.Difficulty {
		if len(selector.result.Actions) >= 24 {
			return selector.result, errors.New("mission selection exceeded its bounded native action count")
		}
		isNext := difficulty < req.Difficulty
		err = selector.waitStep(selectionContext, difficulty, isNext)
		if err != nil {
			return selector.result, fmt.Errorf("selectStepReady: %w", err)
		}
		err = selector.recheck(selectionContext)
		if err != nil {
			return selector.result, fmt.Errorf("selectStepIdentity: %w", err)
		}
		expectedDifficulty := difficulty
		var action any
		if isNext {
			expectedDifficulty++
			action, err = clientManager.NextMission(selectionContext, req.ClientID)
		} else {
			expectedDifficulty--
			action, err = clientManager.PreviousMission(selectionContext, req.ClientID)
		}
		if action != nil {
			selector.result.Actions = append(selector.result.Actions, action)
		}
		if err != nil {
			return selector.result, fmt.Errorf("selectStepAction: %w", err)
		}
		if action == nil {
			return selector.result, errors.New("mission step returned no receipt; do not replay")
		}
		selection, err = selector.waitSelection(selectionContext, req, expectedDifficulty)
		if err != nil {
			return selector.result, fmt.Errorf("selectStepWait: %w", err)
		}
		if selection.Difficulty == nil || *selection.Difficulty != expectedDifficulty {
			return selector.result, errors.New("mission step did not yield its exact expected difficulty; do not replay")
		}
		difficulty = expectedDifficulty
	}
	err = selector.recheck(selectionContext)
	if err != nil {
		return selector.result, fmt.Errorf("selectFinalIdentity: %w", err)
	}
	return selector.result, nil
}

func (e *debugMissionSelector) recheck(ctx context.Context) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("selectContext: %w", err)
	}
	current, err := sampleDebugMissionActor(ctx, e.host, e.clientManager, e.actor.client.ClientID)
	if err != nil {
		return fmt.Errorf("selectProfile: %w", err)
	}
	if !isDebugMissionActionActor(e.actor, current) || current.profile.GameID != 0 {
		return debugMissionActionOwnership("owned process, accepted login, or game changed during mission selection")
	}
	return nil
}

func (e *debugMissionSelector) inspect(ctx context.Context) (*controlMapState, error) {
	err := e.recheck(ctx)
	if err != nil {
		return nil, fmt.Errorf("selectInspectIdentity: %w", err)
	}
	receipt, err := e.clientManager.InspectMap(ctx, e.actor.client.ClientID)
	if receipt != nil {
		e.result.Selection = receipt
	}
	if err != nil {
		return nil, fmt.Errorf("selectInspect: %w", err)
	}
	selection, err := debugMissionSelectionState(receipt)
	if err != nil {
		return nil, fmt.Errorf("selectInspectShape: %w", err)
	}
	if selection.IsAvailable && selection.IsLoaded && selection.State != nil && *selection.State == 5 {
		if selection.GameType != nil && *selection.GameType != 2 {
			return nil, errors.New("mission selection is in a different native mode; no mode switch will be attempted")
		}
		if selection.DifficultyMode != nil && *selection.DifficultyMode != 1 {
			return nil, errors.New("mission selection requires the first campaign difficulty mode (Infection)")
		}
		if selection.Difficulty != nil && (*selection.Difficulty < 1 || *selection.Difficulty > 24) {
			return nil, errors.New("mission selection is outside the first campaign mode")
		}
	}
	return selection, nil
}

func (e *debugMissionSelector) waitEntry(ctx context.Context) (*controlMapState, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		selection, err := e.inspect(ctx)
		if err != nil {
			return nil, fmt.Errorf("selectEntryInspect: %w", err)
		}
		if selection.IsAvailable && selection.IsLoaded && selection.State != nil && *selection.State == 1 && selection.IsCampaignReady {
			return selection, nil
		}
		if selection.isReady(0) && selection.DifficultyMode != nil && *selection.DifficultyMode == 1 {
			return selection, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("selectEntryWait: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (e *debugMissionSelector) waitStep(ctx context.Context, difficulty uint32, isNext bool) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		selection, err := e.inspect(ctx)
		if err != nil {
			return fmt.Errorf("selectStepInspect: %w", err)
		}
		if selection.isReady(0) && selection.Difficulty != nil && *selection.Difficulty != difficulty {
			return errors.New("native selection changed before the planned mission step")
		}
		if selection.isReady(difficulty) && selection.DifficultyMode != nil && *selection.DifficultyMode == 1 && (isNext && selection.IsNextReady || !isNext && selection.IsPreviousReady) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("selectStepWait: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (e *debugMissionSelector) waitSelection(ctx context.Context, req debugMapRequest, difficulty uint32) (*controlMapState, error) {
	req.Difficulty = difficulty
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("selectWaitPayload: %w", err)
	}
	receipt, err := observeDebugMap(ctx, e.clientManager, "client.wait_selection", payload)
	if receipt != nil {
		e.result.Selection = receipt
	}
	if err != nil {
		return nil, fmt.Errorf("selectWaitObservation: %w", err)
	}
	err = e.recheck(ctx)
	if err != nil {
		return nil, fmt.Errorf("selectWaitIdentity: %w", err)
	}
	selection, err := debugMissionSelectionState(receipt)
	if err != nil {
		return nil, fmt.Errorf("selectWaitShape: %w", err)
	}
	if selection.GameType == nil || *selection.GameType != 2 || selection.Difficulty == nil || *selection.Difficulty < 1 || *selection.Difficulty > 24 {
		return nil, errors.New("native selection left the first campaign mode after an action")
	}
	// The original native snapshot may precede publication of the mode scalar.
	// Continue querying without any action until first-mode eligibility is known.
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		selection, err = e.inspect(ctx)
		if err != nil {
			return nil, fmt.Errorf("selectModeInspect: %w", err)
		}
		if selection.isReady(difficulty) && selection.DifficultyMode != nil && *selection.DifficultyMode == 1 {
			return selection, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("selectModeWait: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func debugMissionSelectionState(receipt any) (*controlMapState, error) {
	result, isResult := receipt.(map[string]any)
	if !isResult {
		return nil, errors.New("mission selection receipt has an invalid shape")
	}
	selection, isSelection := result["map_state"].(*controlMapState)
	if !isSelection || selection == nil {
		return nil, errors.New("mission selection receipt is missing native observations")
	}
	return selection, nil
}
