//go:build scenario

package main

import "github.com/yuin/gopher-lua"

func registerDebugLuaScreen(state *lua.LState) {
	meta := state.NewTypeMetatable("ClientScreen")
	state.SetField(meta, "__index", state.NewFunction(debugLuaScreenIndex))
	state.SetField(meta, "__metatable", lua.LString("ClientScreen"))
	screens := state.NewTable()
	screens.RawSetString("None", lua.LString("none"))
	screens.RawSetString("Profile", lua.LString("profile"))
	screens.RawSetString("Inventory", lua.LString("inventory"))
	screens.RawSetString("Friends", lua.LString("friends"))
	screens.RawSetString("Leaderboards", lua.LString("leaderboards"))
	screens.RawSetString("Manual", lua.LString("manual"))
	screens.RawSetString("Galaxy", lua.LString("galaxy"))
	state.SetGlobal("screen", screens)
}

func debugLuaScreenIndex(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	switch state.CheckString(2) {
	case "Snapshot":
		state.Push(state.NewFunction(debugLuaScreenState))
	case "Set":
		state.Push(state.NewFunction(debugLuaScreenSet))
	case "Wait":
		state.Push(state.NewFunction(debugLuaScreenWait))
	case "Close":
		state.Push(state.NewFunction(debugLuaScreenClose))
	default:
		state.Push(lua.LNil)
	}
	return 1
}

func debugLuaScreenState(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.screen_state", map[string]any{"client_id": scope.client.id}, true)
}

func debugLuaScreenSet(state *lua.LState) int {
	return debugLuaScreenRequest(state, "client.set_screen", state.CheckString(2), 3)
}

func debugLuaScreenWait(state *lua.LState) int {
	return debugLuaScreenRequest(state, "client.wait_screen", state.CheckString(2), 3)
}

func debugLuaScreenClose(state *lua.LState) int {
	return debugLuaScreenRequest(state, "client.set_screen", "none", 2)
}

func debugLuaScreenRequest(state *lua.LState, operation, screen string, timeoutIndex int) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	timeoutMS := debugLuaClientTimeout(state, scope.client, timeoutIndex)
	return scope.client.vm.invokeTimed(state, operation, map[string]any{
		"client_id": scope.client.id, "screen": screen, "timeout_ms": timeoutMS,
	}, true, timeoutMS)
}
