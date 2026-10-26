//go:build windows && cgo && scenario && fangdebug

package main

/*
#include "control.h"
*/
import "C"

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

func readControlCinematicReceipt(requestID uint32) *controlCinematicSnapshot {
	var snapshot C.fang_control_cinematic_snapshot
	isFound := C.fang_control_window_cinematic_result(C.uint32_t(requestID), &snapshot)
	if isFound == 0 {
		return nil
	}
	result := &controlCinematicSnapshot{IsAvailable: snapshot.is_available != 0,
		Mode: uint32(snapshot.mode), GameMode: uint32(snapshot.game_mode), CurrentRoom: uint32(snapshot.current_room), NextMode: uint32(snapshot.next_mode),
		IsActive: snapshot.is_active != 0, IsSkippable: snapshot.is_skippable != 0,
		IsFinishedPulse: snapshot.is_finished_pulse != 0, IsTransitioning: snapshot.is_transitioning != 0,
		IsCameraPresent: snapshot.is_camera_present != 0, IsMoviePresent: snapshot.is_movie_present != 0, IsMoviePlaying: snapshot.is_movie_playing != 0}
	if snapshot.fields&1 != 0 {
		token := uint32(snapshot.active_spline_token)
		result.ActiveSplineToken = &token
	}
	return result
}
