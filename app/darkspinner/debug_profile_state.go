//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	serverruntime "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/darkspinnet/darkspin/server/scenario/control"
)

type debugProfileStateRequest struct {
	Name string `json:"name"`
}

type debugLocationRequest struct {
	Name          string `json:"name"`
	Location      string `json:"location"`
	AfterRevision uint64 `json:"after_revision"`
	TimeoutMS     uint32 `json:"timeout_ms"`
}

// Explicit debug response allowlist. Neither a persisted account aggregate nor
// the complete bug-report context is serialized through this endpoint.
type debugProfileState struct {
	Name                      string        `json:"name"`
	UserID                    int64         `json:"user_id"`
	CapturedAt                time.Time     `json:"captured_at"`
	IsAuthenticated           bool          `json:"is_authenticated"`
	SessionID                 string        `json:"session_id"`
	IsUserActive              bool          `json:"is_user_active"`
	GameID                    uint32        `json:"game_id"`
	PlaygroupID               uint32        `json:"playgroup_id"`
	Presence                  debugPresence `json:"presence"`
	IsGameplayActive          bool          `json:"is_gameplay_active"`
	IsGameplayAvailable       bool          `json:"is_gameplay_available"`
	GameplayUnavailableReason string        `json:"gameplay_unavailable_reason,omitempty"`
	GameplayGameID            uint32        `json:"gameplay_game_id"`
	SessionStage              string        `json:"session_stage"`
	IsDungeonSetupFinal       bool          `json:"is_dungeon_setup_final"`
	IsArena                   bool          `json:"is_arena"`
	IsArenaSquadAccepted      bool          `json:"is_arena_squad_accepted"`
	IsArenaCombatActive       bool          `json:"is_arena_combat_active"`
	Mission                   debugMission  `json:"mission"`
}

type debugPresence struct {
	Status   string `json:"status"`
	Source   string `json:"source"`
	Location string `json:"location"`
	Revision uint64 `json:"revision"`
}

type debugMission struct {
	Label      string `json:"label"`
	Asset      string `json:"asset"`
	Difficulty uint32 `json:"difficulty"`
}

func (e *debugHost) ProfileState(ctx context.Context, name string) (debugProfileState, error) {
	e.mutex.Lock()
	credential, isCreated := e.credentialsByName[name]
	e.mutex.Unlock()
	if !isCreated {
		return debugProfileState{}, &control.ScenarioError{Code: control.NotFound, Context: "profileAlias", Message: "profile alias is not owned by this session"}
	}
	select {
	case <-e.done:
		return debugProfileState{}, errors.New("owned debug server has stopped")
	default:
	}
	observation, err := e.server.DebugProfileState(ctx, credential.UserID)
	if err != nil {
		return debugProfileState{}, fmt.Errorf("profileObserve: %w", err)
	}
	return debugProfileStateDTO(name, observation), nil
}

func debugProfileStateDTO(name string, observation serverruntime.DebugProfileState) debugProfileState {
	presenceStatus := "unavailable"
	if observation.IsPresenceAvailable {
		presenceStatus = "available"
	} else if observation.PresenceRevision > 0 && !observation.IsAuthenticated {
		presenceStatus = "stale"
	}
	return debugProfileState{Name: name, UserID: observation.UserID, CapturedAt: observation.CapturedAt,
		IsAuthenticated: observation.IsAuthenticated, SessionID: observation.SessionID, IsUserActive: observation.IsUserActive,
		GameID: observation.GameID, PlaygroupID: observation.PlaygroupID,
		Presence:         debugPresence{Status: presenceStatus, Source: "last_client_reported_presence", Location: observation.LastReportedLocation, Revision: observation.PresenceRevision},
		IsGameplayActive: observation.IsGameplayActive, IsGameplayAvailable: observation.IsGameplayAvailable,
		GameplayUnavailableReason: observation.GameplayUnavailableReason, GameplayGameID: observation.GameplayGameID,
		SessionStage: observation.SessionStage, IsDungeonSetupFinal: observation.IsDungeonSetupFinal,
		IsArena: observation.IsArena, IsArenaSquadAccepted: observation.IsArenaSquadAccepted, IsArenaCombatActive: observation.IsArenaCombatActive,
		Mission: debugMission{Label: observation.MissionLabel, Asset: observation.MissionAsset, Difficulty: observation.MissionDifficulty}}
}

func observeDebugProfile(ctx context.Context, host *debugHost, operation string, payload json.RawMessage) (any, error) {
	if host == nil {
		return nil, errors.New("start the isolated server before observing profiles")
	}
	if operation == "profile.state" {
		var req debugProfileStateRequest
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("stateRequest: %w", err)
		}
		state, err := host.ProfileState(ctx, req.Name)
		if err != nil {
			return state, fmt.Errorf("stateObserve: %w", err)
		}
		return state, nil
	}
	var req debugLocationRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return nil, fmt.Errorf("locationRequest: %w", err)
	}
	switch req.Location {
	case "arsenal", "navigation", "editor":
	default:
		return nil, errors.New("reported location must be arsenal, navigation, or editor")
	}
	if req.TimeoutMS == 0 || req.TimeoutMS > 120000 {
		return nil, errors.New("reported location timeout must be between 1 and 120000 milliseconds")
	}
	waitContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var last debugProfileState
	for {
		last, err = host.ProfileState(waitContext, req.Name)
		if err != nil {
			return last, fmt.Errorf("locationObserve: %w", err)
		}
		if last.IsAuthenticated && last.Presence.Status == "available" && last.Presence.Location == req.Location && last.Presence.Revision > req.AfterRevision {
			return last, nil
		}
		select {
		case <-waitContext.Done():
			return last, &control.ScenarioError{Code: control.DeadlineExceeded, Context: "reportedLocation", Message: "requested client-reported location revision was not observed before the deadline", Cause: waitContext.Err()}
		case <-ticker.C:
		}
	}
}
