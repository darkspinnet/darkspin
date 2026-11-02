//go:build windows && cgo && scenario && fangdebug

package scenario

/*
#include "scenario/control.h"
*/
import "C"

type controlArenaLobbyState struct {
	IsAvailable      bool    `json:"is_available"`
	IsLoaded         bool    `json:"is_loaded"`
	IsReady          bool    `json:"is_ready"`
	SquadIndex       *uint32 `json:"squad_index"`
	IsSquadValid     *bool   `json:"is_squad_valid"`
	IsAcceptDisabled *bool   `json:"is_accept_disabled"`
	IsAcceptVisible  *bool   `json:"is_accept_visible"`
}

func readControlArenaReceipt(requestID uint32) *controlArenaLobbyState {
	var snapshot C.fang_control_arena_state
	isFound := C.fang_control_window_arena_result(C.uint32_t(requestID), &snapshot)
	if isFound == 0 {
		return nil
	}
	fields := uint32(snapshot.fields)
	return &controlArenaLobbyState{
		IsAvailable: snapshot.is_available != 0, IsLoaded: snapshot.is_loaded != 0, IsReady: snapshot.is_ready != 0,
		SquadIndex:       controlMapNumber(fields, 1, uint32(snapshot.squad_index)),
		IsSquadValid:     controlMapBoolean(fields, 2, uint32(snapshot.is_squad_valid)),
		IsAcceptDisabled: controlMapBoolean(fields, 4, uint32(snapshot.is_accept_disabled)),
		IsAcceptVisible:  controlMapBoolean(fields, 8, uint32(snapshot.is_accept_visible)),
	}
}
