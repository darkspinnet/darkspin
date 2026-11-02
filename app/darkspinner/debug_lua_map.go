//go:build scenario

package main

import (
	"math"

	"github.com/yuin/gopher-lua"
)

func debugLuaMissionSelection(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.mission_selection", map[string]any{"client_id": scope.client.id}, true)
}

func debugLuaMissionWaitSelection(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	difficulty := float64(state.OptNumber(2, 0))
	if math.IsNaN(difficulty) || math.IsInf(difficulty, 0) || math.Trunc(difficulty) != difficulty || difficulty < 0 || difficulty > 72 {
		state.ArgError(2, "difficulty must be from 1 to 72, or 0 for any campaign selection")
		return 0
	}
	return scope.client.vm.invoke(state, "client.wait_selection", map[string]any{
		"client_id": scope.client.id, "difficulty": difficulty,
		"timeout_ms": debugLuaClientTimeout(state, scope.client, 3),
	}, true)
}

func debugLuaMissionSelect(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	difficulty := float64(state.CheckNumber(2))
	if math.IsNaN(difficulty) || math.IsInf(difficulty, 0) || math.Trunc(difficulty) != difficulty || difficulty < 1 || difficulty > 24 {
		state.ArgError(2, "difficulty must be an integer from 1 to 24 in the first campaign mode")
		return 0
	}
	return scope.client.vm.invoke(state, "client.select_mission", map[string]any{
		"client_id": scope.client.id, "difficulty": difficulty,
		"timeout_ms": debugLuaClientTimeout(state, scope.client, 3),
	}, true)
}
