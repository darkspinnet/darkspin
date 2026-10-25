//go:build scenario

package main

import "github.com/yuin/gopher-lua"

func debugLuaMissionEnter(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	timeoutMS := debugLuaClientTimeout(state, scope.client, 3)
	return scope.client.vm.invokeTimed(state, "client.enter_mission", map[string]any{
		"client_id": scope.client.id, "difficulty": state.CheckInt(2), "timeout_ms": timeoutMS,
	}, true, timeoutMS)
}

func debugLuaMissionBeamOut(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.vm.invoke(state, "client.beam_out", map[string]any{"client_id": scope.client.id, "timeout_ms": debugLuaClientTimeout(state, scope.client, 2)}, true)
}

func debugLuaMissionDeployment(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.deployment_state", map[string]any{"client_id": scope.client.id}, true)
}

func debugLuaMissionWaitDeploymentReady(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.vm.invoke(state, "client.wait_deployment_ready", map[string]any{
		"client_id": scope.client.id, "timeout_ms": debugLuaClientTimeout(state, scope.client, 2),
	}, true)
}

func debugLuaMissionState(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.mission_state", map[string]any{"client_id": scope.client.id}, true)
}

func debugLuaMissionWait(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.vm.invoke(state, "client.wait_mission", map[string]any{
		"client_id": scope.client.id, "timeout_ms": debugLuaClientTimeout(state, scope.client, 2),
	}, true)
}

func debugLuaMissionWaitDeployment(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.vm.invoke(state, "client.wait_deployment", map[string]any{
		"client_id": scope.client.id, "timeout_ms": debugLuaClientTimeout(state, scope.client, 2),
	}, true)
}

func debugLuaMissionStart(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.vm.invoke(state, "client.start_mission", map[string]any{
		"client_id": scope.client.id, "timeout_ms": debugLuaClientTimeout(state, scope.client, 2),
	}, true)
}

func debugLuaMissionContinue(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.vm.invoke(state, "client.continue_mission", map[string]any{
		"client_id": scope.client.id, "timeout_ms": debugLuaClientTimeout(state, scope.client, 2),
	}, true)
}
