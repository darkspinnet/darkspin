//go:build scenario

package main

import (
	"math"

	"github.com/yuin/gopher-lua"
)

func debugLuaDebugPause(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	durationMS := float64(state.OptNumber(2, 2000))
	if math.IsNaN(durationMS) || math.IsInf(durationMS, 0) || math.Trunc(durationMS) != durationMS || durationMS < 1 || durationMS > 5000 {
		state.ArgError(2, "debugger pause duration must be an integer from 1 through 5000 milliseconds")
		return 0
	}
	return scope.client.invoke(state, "client.debug_pause", map[string]any{"client_id": scope.client.id, "duration_ms": uint32(durationMS)}, true)
}

func debugLuaDebugResume(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.debug_resume", map[string]any{"client_id": scope.client.id, "pause_id": state.CheckString(2)}, true)
}

func debugLuaDebugPauseState(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.debug_pause_state", map[string]any{"client_id": scope.client.id}, true)
}
