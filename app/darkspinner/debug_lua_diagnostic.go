//go:build scenario

package main

import "github.com/yuin/gopher-lua"

func debugLuaDiagnosticCapture(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.capture", map[string]any{
		"client_id": scope.client.id, "label": state.CheckString(2),
	}, true)
}
