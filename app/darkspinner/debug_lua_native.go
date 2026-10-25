//go:build scenario

package main

import "github.com/yuin/gopher-lua"

func debugLuaWorldNativeState(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.native_state", map[string]any{"client_id": scope.client.id}, true)
}

func debugLuaWorldProject(state *lua.LState) int {
	return debugLuaNativePoint(state, "client.project_world")
}

func debugLuaWorldClick(state *lua.LState) int {
	return debugLuaNativePoint(state, "client.click_world")
}

func debugLuaWorldHold(state *lua.LState) int {
	return debugLuaNativePoint(state, "client.hold_world")
}

func debugLuaNativePoint(state *lua.LState, operation string) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	point := state.CheckTable(2)
	coordinates := make(map[string]any, 3)
	for _, axisName := range []string{"x", "y", "z"} {
		coordinate, isNumber := point.RawGetString(axisName).(lua.LNumber)
		if !isNumber {
			state.ArgError(2, "world point requires numeric x, y and z")
			return 0
		}
		coordinates[axisName] = float64(coordinate)
	}
	req := map[string]any{"client_id": scope.client.id, "point": coordinates}
	if operation == "client.hold_world" {
		req["hold_ms"] = state.CheckInt(3)
	}
	return scope.client.invoke(state, operation, req, true)
}
