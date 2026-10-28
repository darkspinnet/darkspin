//go:build scenario

package main

import (
	"github.com/yuin/gopher-lua"
	"math"
)

func debugLuaWorldRestoreHero(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.restore_hero", map[string]any{"client_id": scope.client.id}, true)
}

func debugLuaWorldPlaceHero(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	point := debugLuaPathPoint(state, 2)
	if point == nil {
		return 0
	}
	return scope.client.invoke(state, "client.place_hero", map[string]any{"client_id": scope.client.id, "point": point}, true)
}

func debugLuaWorldReplaceHero(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	noun := float64(state.CheckNumber(2))
	if math.IsNaN(noun) || math.IsInf(noun, 0) || math.Trunc(noun) != noun || noun < 1 || noun > math.MaxUint32 {
		state.ArgError(2, "hero noun must be an installed uint32 template ID")
		return 0
	}
	return scope.client.invoke(state, "client.replace_hero", map[string]any{"client_id": scope.client.id, "noun": uint32(noun)}, true)
}
