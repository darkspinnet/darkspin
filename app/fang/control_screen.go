//go:build windows && cgo && scenario && fangdebug

package main

/*
#include "control.h"
*/
import "C"

type controlScreenState struct {
	IsAvailable           bool   `json:"is_available"`
	Screen                string `json:"screen"`
	IsInventoryOpen       bool   `json:"is_inventory_open"`
	IsBrowserOpen         bool   `json:"is_browser_open"`
	NativeScreenID        *int32 `json:"native_screen_id,omitempty"`
	IsContentReady        *bool  `json:"is_content_ready,omitempty"`
	OwnerGeneration       uint64 `json:"owner_generation"`
	SelectionSource       string `json:"selection_source,omitempty"`
	IsInvocationAttempted bool   `json:"is_invocation_attempted"`
	IsInvocationReturned  bool   `json:"is_invocation_returned"`
}

func controlScreenNumber(screen string) (uint32, bool) {
	switch screen {
	case "none":
		return 0, true
	case "profile":
		return 1, true
	case "inventory":
		return 2, true
	case "friends":
		return 3, true
	case "leaderboards":
		return 4, true
	case "manual":
		return 5, true
	case "galaxy":
		return 7, true
	default:
		return 0, false
	}
}

func readControlScreenReceipt(requestID uint32) *controlScreenState {
	var snapshot C.fang_control_screen_state
	isFound := C.fang_control_window_screen_result(C.uint32_t(requestID), &snapshot)
	if isFound == 0 {
		return nil
	}
	screens := [...]string{"none", "profile", "inventory", "friends", "leaderboards", "manual", "unsupported", "galaxy"}
	screen := "unsupported"
	if uint32(snapshot.screen) < uint32(len(screens)) {
		screen = screens[uint32(snapshot.screen)]
	}
	state := &controlScreenState{IsAvailable: snapshot.is_available != 0, Screen: screen,
		IsInventoryOpen: snapshot.is_inventory_open != 0, IsBrowserOpen: snapshot.is_browser_open != 0,
		OwnerGeneration: uint64(snapshot.owner_generation), IsInvocationAttempted: snapshot.is_invocation_attempted != 0,
		IsInvocationReturned: snapshot.is_invocation_returned != 0}
	if snapshot.fields&1 != 0 {
		nativeID := int32(snapshot.native_screen_id)
		state.NativeScreenID = &nativeID
	}
	if state.IsAvailable && state.Screen != "unsupported" {
		state.SelectionSource = "native"
		if snapshot.fields&2 != 0 {
			state.SelectionSource = "dom"
		}
	}
	// Active native/DOM selection does not establish network or render readiness.
	return state
}
