//go:build scenario

package main

import "github.com/yuin/gopher-lua"

func debugLuaAbilityCancel(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.cancel_ability", map[string]any{"client_id": scope.client.id}, true)
}

func debugLuaAbilityState(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.ability_state", map[string]any{"client_id": scope.client.id}, true)
}

func debugLuaAbilityCast(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	point := state.CheckTable(2)
	coordinates := make(map[string]any, 3)
	for _, axisName := range []string{"x", "y", "z"} {
		coordinate, isNumber := point.RawGetString(axisName).(lua.LNumber)
		if !isNumber {
			state.ArgError(2, "ability point requires numeric x, y and z")
			return 0
		}
		coordinates[axisName] = float64(coordinate)
	}
	return scope.client.invoke(state, "client.cast_active", map[string]any{"client_id": scope.client.id, "point": coordinates, "timeout_ms": debugLuaClientTimeout(state, scope.client, 3)}, true)
}
