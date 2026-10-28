//go:build scenario

package main

import "github.com/yuin/gopher-lua"

func debugLuaWorldStatus(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.world_state", map[string]any{"client_id": scope.client.id}, true)
}

func debugLuaWorldReady(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	timeoutMS := debugLuaClientTimeout(state, scope.client, 2)
	return scope.client.vm.invokeTimed(state, "client.wait_world_ready", map[string]any{
		"client_id": scope.client.id, "timeout_ms": timeoutMS,
	}, true, timeoutMS)
}

func debugLuaWorldLiving(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.world_living", map[string]any{"client_id": scope.client.id}, true)
}

func debugLuaWorldNPCCatalog(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.npc_catalog", map[string]any{"client_id": scope.client.id}, true)
}

func debugLuaWorldConfigure(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	fields, failure := debugLuaResourceRequest(state, "client.configure_world")
	if failure != nil {
		return scope.client.vm.returnOperation(state, nil, failure, true)
	}
	fields["client_id"] = scope.client.id
	return scope.client.invoke(state, "client.configure_world", fields, true)
}

func debugLuaWorldSpawn(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.spawn_npc", map[string]any{"client_id": scope.client.id, "noun": state.CheckString(2), "timeout_ms": debugLuaClientTimeout(state, scope.client, 3)}, true)
}

func debugLuaWorldSpawnAsync(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.spawn_npc_async", map[string]any{"client_id": scope.client.id, "noun": state.CheckString(2)}, true)
}

func debugLuaWorldSpawnState(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.spawn_state", map[string]any{"client_id": scope.client.id, "request_id": state.CheckString(2)}, true)
}
