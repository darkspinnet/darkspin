//go:build scenario

package main

import "github.com/yuin/gopher-lua"

func debugLuaDiagnosticWaitForSignal(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	timeoutMS := debugLuaClientTimeout(state, scope.client, 3)
	return scope.client.vm.invokeTimed(state, "client.wait_signal", map[string]any{
		"client_id": scope.client.id, "name": state.CheckString(2),
	}, true, timeoutMS)
}

func debugLuaDiagnosticCapture(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.capture", map[string]any{
		"client_id": scope.client.id, "label": state.CheckString(2),
	}, true)
}
