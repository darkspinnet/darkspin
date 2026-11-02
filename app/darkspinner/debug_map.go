//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario/control"
)

// A copied allowlist from the owned native map UI. Nil means unobserved, never false.
type controlMapState struct {
	IsAvailable              bool    `json:"is_available"`
	IsLoaded                 bool    `json:"is_loaded"`
	IsCampaignReady          bool    `json:"is_campaign_ready"`
	IsPreviousReady          bool    `json:"is_previous_ready"`
	IsNextReady              bool    `json:"is_next_ready"`
	DifficultyMode           *uint32 `json:"difficulty_mode"`
	State                    *uint32 `json:"state"`
	Difficulty               *uint32 `json:"difficulty"`
	GameType                 *uint32 `json:"game_type"`
	PVPGameSize              *uint32 `json:"pvp_game_size"`
	PVPRankType              *uint32 `json:"pvp_rank_type"`
	PartyMemberCount         *uint32 `json:"party_member_count"`
	IsPVPUnlocked            *bool   `json:"is_pvp_unlocked"`
	IsPartyLeader            *bool   `json:"is_party_leader"`
	ArePVPMembersReady       *bool   `json:"are_pvp_members_ready"`
	AnimationState           *uint32 `json:"animation_state"`
	AnimationDifficulty      *uint32 `json:"animation_difficulty"`
	IsTransactionsProcessing *bool   `json:"is_transactions_processing"`
	ArePartyMembersReady     *bool   `json:"are_party_members_ready"`
	IsStartDisabled          *bool   `json:"is_start_disabled"`
	IsStartVisible           *bool   `json:"is_start_visible"`
}

type debugMapRequest struct {
	ClientID   string `json:"client_id"`
	Difficulty uint32 `json:"difficulty,omitempty"`
	TimeoutMS  uint32 `json:"timeout_ms,omitempty"`
}

func observeDebugMap(ctx context.Context, clientManager *debugClientManager, operation string, payload json.RawMessage) (any, error) {
	if operation != "client.mission_selection" && operation != "client.wait_selection" {
		return nil, errors.New("unsupported mission selection observation")
	}
	if clientManager == nil {
		return nil, errors.New("mission selection requires an owned client manager")
	}
	var req debugMapRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return nil, fmt.Errorf("selectionRequest: %w", err)
	}
	if req.ClientID == "" || req.Difficulty > 72 || req.TimeoutMS > 120000 || operation == "client.wait_selection" && req.TimeoutMS == 0 {
		return nil, errors.New("selection requires a client ID, optional difficulty from 1 to 72, and waits require a timeout from 1 to 120000 milliseconds")
	}
	if req.TimeoutMS != 0 {
		waitContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
		defer cancel()
		ctx = waitContext
	}
	client, err := clientManager.State(req.ClientID)
	if err != nil {
		return nil, fmt.Errorf("selectionClient: %w", err)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var result any
	for {
		current, stateErr := clientManager.State(req.ClientID)
		if stateErr != nil {
			return result, fmt.Errorf("selectionLiveness: %w", stateErr)
		}
		if current.InstanceID != client.InstanceID || current.ProcessID != client.ProcessID || !current.IsRunning || current.Status == "liveness_unavailable" {
			return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "selectionIdentity", Message: "owned client changed or stopped during selection observation"}
		}
		result, err = clientManager.InspectMap(ctx, req.ClientID)
		if err != nil {
			return result, fmt.Errorf("selectionProbe: %w", err)
		}
		if operation == "client.mission_selection" {
			return result, nil
		}
		receipt, isReceipt := result.(map[string]any)
		if !isReceipt {
			return result, errors.New("mission selection receipt has an invalid shape")
		}
		selection, isSelection := receipt["map_state"].(*controlMapState)
		if !isSelection || selection == nil {
			return result, errors.New("mission selection receipt is missing native observations")
		}
		if selection.isReady(req.Difficulty) {
			return result, nil
		}
		select {
		case <-ctx.Done():
			code := control.DeadlineExceeded
			if errors.Is(ctx.Err(), context.Canceled) {
				code = control.ErrorCode("cancelled")
			}
			return result, &control.ScenarioError{Code: code, Context: "selectionWait", Message: "stable enabled campaign selection was not observed", Cause: ctx.Err()}
		case <-ticker.C:
		}
	}
}

func (e *controlMapState) isReady(difficulty uint32) bool {
	if !e.IsAvailable || !e.IsLoaded || e.State == nil || *e.State != 5 || e.GameType == nil || *e.GameType != 2 ||
		e.Difficulty == nil || difficulty != 0 && *e.Difficulty != difficulty || e.AnimationState == nil || *e.AnimationState != 0 ||
		e.AnimationDifficulty == nil || *e.AnimationDifficulty != *e.Difficulty {
		return false
	}
	return e.IsTransactionsProcessing != nil && !*e.IsTransactionsProcessing && e.ArePartyMembersReady != nil && *e.ArePartyMembersReady &&
		e.IsStartVisible != nil && *e.IsStartVisible && e.IsStartDisabled != nil && !*e.IsStartDisabled
}
