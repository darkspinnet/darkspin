//go:build scenario

package main

import (
	"math"

	"github.com/yuin/gopher-lua"
)

func debugLuaWorldPath(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	start := debugLuaPathPoint(state, 2)
	if start == nil {
		return 0
	}
	goal := debugLuaPathPoint(state, 3)
	if goal == nil {
		return 0
	}
	return scope.client.invoke(state, "client.world_path", map[string]any{"client_id": scope.client.id, "start": start, "goal": goal}, true)
}

func debugLuaPathPoint(state *lua.LState, index int) map[string]any {
	point := state.CheckTable(index)
	coordinates := make(map[string]any, 3)
	for _, axisName := range []string{"x", "y", "z"} {
		coordinate, isNumber := point.RawGetString(axisName).(lua.LNumber)
		if !isNumber || math.IsNaN(float64(coordinate)) || math.IsInf(float64(coordinate), 0) || math.Abs(float64(coordinate)) > math.MaxFloat32 {
			state.ArgError(index, "path point requires finite numeric x, y and z")
			return nil
		}
		coordinates[axisName] = float64(coordinate)
	}
	return coordinates
}
