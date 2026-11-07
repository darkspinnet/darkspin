//go:build scenario

package main

import "github.com/yuin/gopher-lua"

func debugLuaChatIndex(state *lua.LState, name string) int {
	switch name {
	case "Snapshot":
		state.Push(state.NewFunction(debugLuaChatSnapshot))
	case "Wait":
		state.Push(state.NewFunction(debugLuaChatWait))
	case "WaitNative":
		state.Push(state.NewFunction(debugLuaChatWaitNative))
	case "Artifacts":
		state.Push(state.NewFunction(debugLuaChatArtifacts))
	default:
		state.Push(lua.LNil)
	}
	return 1
}

func debugLuaChatWaitNative(state *lua.LState) int {
	client := debugLuaScopeSelf(state).client
	cursor, err := debugLuaJSON(state.CheckTable(2), 0)
	if err != nil {
		return client.vm.returnOperation(state, nil, debugErrorDTO(err, 0), true)
	}
	timeoutMS := debugLuaClientTimeout(state, client, 4)
	return client.vm.invokeTimed(state, "client.chat_wait_native", map[string]any{
		"client_id": client.id, "cursor": cursor, "event": state.CheckString(3), "timeout_ms": timeoutMS,
	}, true, timeoutMS)
}

func debugLuaChatArtifacts(state *lua.LState) int {
	client := debugLuaScopeSelf(state).client
	return client.invoke(state, "client.chat_artifacts", map[string]any{"client_id": client.id}, true)
}

func debugLuaChatSnapshot(state *lua.LState) int {
	client := debugLuaScopeSelf(state).client
	return client.invoke(state, "client.chat_snapshot", map[string]any{"client_id": client.id}, true)
}

func debugLuaChatWait(state *lua.LState) int {
	client := debugLuaScopeSelf(state).client
	cursor, err := debugLuaJSON(state.CheckTable(2), 0)
	if err != nil {
		return client.vm.returnOperation(state, nil, debugErrorDTO(err, 0), true)
	}
	timeoutMS := debugLuaClientTimeout(state, client, 4)
	return client.vm.invokeTimed(state, "client.chat_wait", map[string]any{
		"client_id": client.id, "cursor": cursor, "command": state.CheckString(3), "timeout_ms": timeoutMS,
	}, true, timeoutMS)
}
