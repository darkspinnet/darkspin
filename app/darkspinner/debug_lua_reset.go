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
	req := map[string]any{"client_id": scope.client.id}
	if state.GetTop() >= 2 && state.Get(2) != lua.LNil {
		options := state.CheckTable(2)
		for _, name := range []string{"hit_point", "maximum_hit_point", "mana_point"} {
			amount := options.RawGetString(name)
			if amount != lua.LNil {
				number, isNumber := amount.(lua.LNumber)
				if !isNumber || math.IsNaN(float64(number)) || math.IsInf(float64(number), 0) {
					state.ArgError(2, "resource fields must be finite numbers")
					return 0
				}
				req[name] = float64(number)
			}
		}
	}
	return scope.client.invoke(state, "client.restore_hero", req, true)
}

func debugLuaWorldResetCooldowns(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.reset_cooldowns", map[string]any{"client_id": scope.client.id}, true)
}

func debugLuaWorldSetAbilityRank(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	rank := float64(state.CheckNumber(2))
	if math.IsNaN(rank) || math.IsInf(rank, 0) || math.Trunc(rank) != rank || rank < 0 || rank > 8 {
		state.ArgError(2, "server fixture rank must be an integer0 through8; zero clears")
		return 0
	}
	return scope.client.invoke(state, "client.set_ability_rank", map[string]any{"client_id": scope.client.id, "ability_rank": int32(rank)}, true)
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
