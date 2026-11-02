//go:build scenario

package main

import lua "github.com/yuin/gopher-lua"

func debugLuaWorldActivateSpoffit(state *lua.LState) int {
	return debugLuaWorldSpoffit(state, "client.activate_spoffit_crystal")
}

func debugLuaWorldDeactivateSpoffit(state *lua.LState) int {
	return debugLuaWorldSpoffit(state, "client.deactivate_spoffit_crystal")
}

func debugLuaWorldSpoffit(state *lua.LState, operation string) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, operation, map[string]any{
		"client_id": scope.client.id, "noun": "Spoffit.Noun",
		"timeout_ms": debugLuaClientTimeout(state, scope.client, 2),
	}, true)
}
