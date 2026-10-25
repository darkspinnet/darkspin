//go:build scenario

package main

import (
	"math"

	"github.com/yuin/gopher-lua"
)

func debugLuaWorldSelectHero(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	slot := float64(state.CheckNumber(2))
	if math.IsNaN(slot) || math.IsInf(slot, 0) || math.Trunc(slot) != slot || slot < 0 || slot > 2 {
		state.ArgError(2, "hero slot must be a zero-based integer from 0 through 2")
		return 0
	}
	return scope.client.invoke(state, "client.select_hero", map[string]any{"client_id": scope.client.id, "slot": uint32(slot), "timeout_ms": debugLuaClientTimeout(state, scope.client, 3)}, true)
}
