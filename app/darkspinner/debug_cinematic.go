//go:build scenario

package main

import (
	"context"
	"errors"
	"fmt"
)

// controlCinematicSnapshot is an observation of build 103 on the owned HWND
// thread. FinishedPulse is transient; its absence does not prove completion.
type controlCinematicSnapshot struct {
	IsAvailable       bool    `json:"is_available"`
	Mode              uint32  `json:"mode"`
	GameMode          uint32  `json:"game_mode"`
	CurrentRoom       uint32  `json:"current_room"`
	NextMode          uint32  `json:"next_mode"`
	IsActive          bool    `json:"is_active"`
	IsSkippable       bool    `json:"is_skippable"`
	IsFinishedPulse   bool    `json:"is_finished_pulse"`
	IsTransitioning   bool    `json:"is_transitioning"`
	IsCameraPresent   bool    `json:"is_camera_present"`
	IsMoviePresent    bool    `json:"is_movie_present"`
	IsMoviePlaying    bool    `json:"is_movie_playing"`
	ActiveSplineToken *uint32 `json:"active_spline_token,omitempty"`
	CompletedTickMS   uint64  `json:"completed_tick_ms"`
}

func (e *debugClientManager) SnapshotCinematic(ctx context.Context, clientID string) (*controlCinematicSnapshot, error) {
	result, err := e.inspectCinematic(ctx, clientID)
	if err != nil {
		return nil, fmt.Errorf("cinematicInspect: %w", err)
	}
	receipt, isReceipt := result.(map[string]any)
	if !isReceipt {
		return nil, errors.New("cinematic receipt is unavailable")
	}
	snapshot, isSnapshot := receipt["cinematic_snapshot"].(*controlCinematicSnapshot)
	if !isSnapshot || snapshot == nil {
		return nil, errors.New("cinematic snapshot is unavailable")
	}
	return snapshot, nil
}

type debugCinematicRequest struct {
	ClientID           string `json:"client_id"`
	IsIntroSkipped     *bool  `json:"is_intro_skipped,omitempty"`
	IsCinematicSkipped *bool  `json:"is_cinematic_skipped,omitempty"`
}

func validateDebugCinematic(req debugCinematicRequest) error {
	if req.ClientID == "" || req.IsIntroSkipped == nil && req.IsCinematicSkipped == nil {
		return errors.New("cinematic configuration requires an owned client ID and at least one skip setting")
	}
	return nil
}
