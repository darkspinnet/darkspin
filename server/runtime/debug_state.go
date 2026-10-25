//go:build scenario

package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/blaze"
	"github.com/darkspinnet/darkspin/server/scenario"
)

// DebugProfileState is a read-only view of independently sampled server sources.
// CapturedAt is the collection time, not the client's presence publication time.
// LastReportedLocation is never a native scene or proof of world readiness.
// Empty fields and false availability represent absent/unsupported observations.
type DebugProfileState struct {
	CapturedAt                time.Time
	UserID                    int64
	IsAuthenticated           bool
	SessionID                 string
	IsUserActive              bool
	GameID                    uint32
	PlaygroupID               uint32
	LastReportedLocation      string
	PresenceRevision          uint64
	IsPresenceAvailable       bool
	IsGameplayActive          bool
	IsGameplayAvailable       bool
	GameplayUnavailableReason string
	GameplayGameID            uint32
	SessionStage              string
	MissionLabel              string
	MissionAsset              string
	MissionDifficulty         uint32
	IsDungeonSetupFinal       bool
	IsArena                   bool
	IsArenaSquadAccepted      bool
	IsArenaCombatActive       bool
}

type debugGameplayStateProvider interface {
	ScenarioGameplayState(context.Context, int64, uint32) (scenario.GameplayState, error)
}

// DebugProfileState observes only an identity already selected by the caller.
// Its adapter must authorize run ownership before calling; it never loads a
// saved account, creates membership, activates a session, or advances gameplay.
func (e *Server) DebugProfileState(ctx context.Context, userID int64) (DebugProfileState, error) {
	if ctx == nil {
		return DebugProfileState{}, errors.New("debug state requires a context")
	}
	err := ctx.Err()
	if err != nil {
		return DebugProfileState{}, fmt.Errorf("stateContext: %w", err)
	}
	if userID <= 0 {
		return DebugProfileState{}, errors.New("debug state requires a positive owned user identity")
	}
	if e == nil || e.userManager == nil {
		return DebugProfileState{}, errors.New("debug state service unavailable")
	}
	observation, err := e.ScenarioObservation(ctx, userID, scenario.Authenticated)
	if err != nil {
		return DebugProfileState{}, fmt.Errorf("stateLogin: %w", err)
	}
	result := DebugProfileState{CapturedAt: time.Now().UTC(), UserID: userID,
		IsAuthenticated: observation.Outcome == scenario.Passed, SessionID: observation.SessionID,
		GameplayUnavailableReason: "gameplay observation requires accepted authentication and an active user"}
	var presenceRegistry *blaze.PresenceRegistry
	switch provider := e.reportContext.(type) {
	case reportContextProvider:
		presenceRegistry = provider.presence
	case *reportContextProvider:
		if provider != nil {
			presenceRegistry = provider.presence
		}
	}
	presence := presenceRegistry.ObserveScenarioPresence(userID)
	result.LastReportedLocation = presence.Location
	result.PresenceRevision = presence.Revision
	// Retained presence survives logout. Keep its label for diagnostics while
	// explicitly withholding availability when accepted authentication is absent.
	result.IsPresenceAvailable = presence.IsAvailable && result.IsAuthenticated
	user := e.userManager.UserByID(userID)
	result.IsUserActive = user != nil
	if !result.IsAuthenticated || user == nil {
		return result, nil
	}
	result.GameID = user.CurrentGameID()
	result.PlaygroupID = user.CurrentPlaygroupID()
	provider, isSupported := e.bugContext.(debugGameplayStateProvider)
	if !isSupported {
		result.GameplayUnavailableReason = "gameplay lifecycle observer is unavailable"
		return result, nil
	}
	gameplay, err := provider.ScenarioGameplayState(ctx, userID, result.GameID)
	if err != nil {
		// An optional lifecycle observation must not erase independent login,
		// presence, and user membership samples needed to diagnose the handshake.
		result.GameplayUnavailableReason = fmt.Sprintf("stateGameplay: %v", err)
		return result, nil
	}
	result.IsGameplayAvailable = gameplay.IsAvailable
	result.GameplayUnavailableReason = gameplay.UnavailableReason
	if !gameplay.IsAvailable {
		return result, nil
	}
	result.IsGameplayActive = gameplay.IsGameplayActive
	if !gameplay.IsGameplayActive {
		return result, nil
	}
	result.SessionStage = gameplay.SessionStage
	result.GameplayGameID = gameplay.GameID
	result.MissionLabel = gameplay.MissionLabel
	result.MissionAsset = gameplay.MissionAsset
	result.MissionDifficulty = gameplay.MissionDifficulty
	result.IsDungeonSetupFinal = gameplay.IsDungeonSetupFinal
	result.IsArena = gameplay.IsArena
	result.IsArenaSquadAccepted = gameplay.IsArenaSquadAccepted
	result.IsArenaCombatActive = gameplay.IsArenaCombatActive
	return result, nil
}
