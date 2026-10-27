//go:build scenario

package main

import (
	"math"
	"time"

	"github.com/yuin/gopher-lua"
)

type debugLuaClient struct {
	vm              *debugLuaVM
	id              string
	processID       uint32
	instanceID      string
	profileName     string
	timeoutMS       int
	startedAt       time.Time
	initialState    lua.LValue
	sceneScope      *lua.LUserData
	uiScope         *lua.LUserData
	screenScope     *lua.LUserData
	cinematicScope  *lua.LUserData
	missionScope    *lua.LUserData
	partyScope      *lua.LUserData
	diagnosticScope *lua.LUserData
	worldScope      *lua.LUserData
	debugScope      *lua.LUserData
	abilityScope    *lua.LUserData
	pvpScope        *lua.LUserData
}

func debugLuaClientIndex(state *lua.LState) int {
	userdata := state.CheckUserData(1)
	client, isClient := userdata.Value.(*debugLuaClient)
	if !isClient {
		state.ArgError(1, "managed client required")
		return 0
	}
	switch state.CheckString(2) {
	case "PVP":
		if client.pvpScope == nil {
			client.pvpScope = debugLuaClientScope(state, client, "pvp")
		}
		state.Push(client.pvpScope)
	case "Ability":
		if client.abilityScope == nil {
			client.abilityScope = debugLuaClientScope(state, client, "ability")
		}
		state.Push(client.abilityScope)
	case "Debug":
		if client.debugScope == nil {
			client.debugScope = debugLuaClientScope(state, client, "debug")
		}
		state.Push(client.debugScope)
	case "World":
		if client.worldScope == nil {
			client.worldScope = debugLuaClientScope(state, client, "world")
		}
		state.Push(client.worldScope)
	case "Diagnostics":
		if client.diagnosticScope == nil {
			client.diagnosticScope = debugLuaClientScope(state, client, "diagnostic")
		}
		state.Push(client.diagnosticScope)
	case "Party":
		if client.partyScope == nil {
			client.partyScope = debugLuaClientScope(state, client, "party")
		}
		state.Push(client.partyScope)
	case "Mission":
		if client.missionScope == nil {
			client.missionScope = debugLuaClientScope(state, client, "mission")
		}
		state.Push(client.missionScope)
	case "Scene":
		if client.sceneScope == nil {
			client.sceneScope = debugLuaClientScope(state, client, "scene")
		}
		state.Push(client.sceneScope)
	case "UI":
		if client.uiScope == nil {
			client.uiScope = debugLuaClientScope(state, client, "ui")
		}
		state.Push(client.uiScope)
	case "Cinematic":
		if client.cinematicScope == nil {
			client.cinematicScope = debugLuaClientScope(state, client, "cinematic")
		}
		state.Push(client.cinematicScope)
	case "ID":
		state.Push(lua.LString(client.id))
	case "PID":
		state.Push(lua.LNumber(client.processID))
	case "InstanceID":
		state.Push(lua.LString(client.instanceID))
	case "InitialSnapshot":
		state.Push(client.initialState)
	case "Snapshot":
		state.Push(state.NewFunction(debugLuaManagedState))
	case "Time":
		state.Push(state.NewFunction(debugLuaManagedTime))
	case "Window":
		state.Push(state.NewFunction(debugLuaManagedWindow))
	case "Click":
		state.Push(state.NewFunction(debugLuaManagedClick))
	case "Login", "WaitForLogin":
		state.Push(state.NewFunction(debugLuaManagedLogin))
	case "SetTimeout":
		state.Push(state.NewFunction(debugLuaManagedSetTimeout))
	case "Ready":
		state.Push(state.NewFunction(debugLuaManagedReady))
	case "ProfileSnapshot":
		state.Push(state.NewFunction(debugLuaManagedProfile))
	case "WaitForReportedLocation":
		state.Push(state.NewFunction(debugLuaManagedLocation))
	case "Close":
		state.Push(state.NewFunction(debugLuaManagedClose))
	case "Terminate":
		state.Push(state.NewFunction(debugLuaManagedTerminate))
	case "Screenshot":
		state.Push(state.NewFunction(debugLuaManagedScreenshot))
	default:
		state.Push(lua.LNil)
	}
	return 1
}

func debugLuaManagedTime(state *lua.LState) int {
	client := debugLuaManagedClient(state)
	state.Push(lua.LNumber(float64(time.Since(client.startedAt)) / float64(time.Millisecond)))
	return 1
}

func (e *debugLuaClient) invoke(state *lua.LState, operation string, req any, isResult bool) int {
	return e.vm.invokeTimed(state, operation, req, isResult, e.timeoutMS)
}

func debugLuaManagedLogin(state *lua.LState) int {
	client := debugLuaManagedClient(state)
	return client.vm.invoke(state, "profile.wait_login", map[string]any{"name": client.profileName, "timeout_ms": debugLuaClientTimeout(state, client, 2)}, true)
}

func debugLuaClientTimeout(state *lua.LState, client *debugLuaClient, index int) int {
	return debugLuaTimeoutMilliseconds(state, index, client.timeoutMS)
}

func debugLuaTimeoutMilliseconds(state *lua.LState, index, defaultMS int) int {
	timeoutMS := float64(state.OptNumber(index, lua.LNumber(defaultMS)))
	if math.IsNaN(timeoutMS) || math.IsInf(timeoutMS, 0) || math.Trunc(timeoutMS) != timeoutMS || timeoutMS < 1 || timeoutMS > 120000 {
		state.ArgError(index, "timeout must be between 1 and 120000 milliseconds")
	}
	return int(timeoutMS)
}

func debugLuaManagedSetTimeout(state *lua.LState) int {
	client := debugLuaManagedClient(state)
	client.timeoutMS = debugLuaTimeoutMilliseconds(state, 2, 0)
	state.Push(lua.LNil)
	return 1
}

func debugLuaManagedReady(state *lua.LState) int {
	client := debugLuaManagedClient(state)
	return client.vm.invoke(state, "client.ready", map[string]any{"client_id": client.id, "timeout_ms": debugLuaClientTimeout(state, client, 2)}, true)
}

func debugLuaManagedProfile(state *lua.LState) int {
	client := debugLuaManagedClient(state)
	return client.invoke(state, "profile.state", map[string]any{"name": client.profileName}, true)
}

func debugLuaManagedLocation(state *lua.LState) int {
	client := debugLuaManagedClient(state)
	state.Remove(1)
	state.Insert(lua.LString(client.profileName), 1)
	return client.vm.waitForReportedLocation(state)
}

func debugLuaManagedClient(state *lua.LState) *debugLuaClient {
	userdata := state.CheckUserData(1)
	client, isClient := userdata.Value.(*debugLuaClient)
	if !isClient {
		state.ArgError(1, "managed client required")
	}
	return client
}

func debugLuaManagedClick(state *lua.LState) int {
	userdata := state.CheckUserData(1)
	client, isClient := userdata.Value.(*debugLuaClient)
	if !isClient {
		state.ArgError(1, "managed client required")
		return 0
	}
	return client.invoke(state, "client.click", map[string]any{"client_id": client.id, "x": state.CheckInt(2), "y": state.CheckInt(3), "button": "left"}, true)
}

func debugLuaManagedWindow(state *lua.LState) int {
	userdata := state.CheckUserData(1)
	client, isClient := userdata.Value.(*debugLuaClient)
	if !isClient {
		state.ArgError(1, "managed client required")
		return 0
	}
	return client.invoke(state, "client.window", map[string]any{"client_id": client.id}, true)
}

func debugLuaManagedScreenshot(state *lua.LState) int {
	userdata := state.CheckUserData(1)
	client, isClient := userdata.Value.(*debugLuaClient)
	if !isClient {
		state.ArgError(1, "managed client required")
		return 0
	}
	return client.invoke(state, "client.screenshot", map[string]any{"client_id": client.id, "label": state.CheckString(2)}, true)
}

func debugLuaManagedState(state *lua.LState) int {
	userdata := state.CheckUserData(1)
	client, isClient := userdata.Value.(*debugLuaClient)
	if !isClient {
		state.ArgError(1, "managed client required")
		return 0
	}
	return client.invoke(state, "client.state", map[string]any{"client_id": client.id}, true)
}

func debugLuaManagedClose(state *lua.LState) int {
	userdata := state.CheckUserData(1)
	client, isClient := userdata.Value.(*debugLuaClient)
	if !isClient {
		state.ArgError(1, "managed client required")
		return 0
	}
	return client.invoke(state, "client.close", map[string]any{"client_id": client.id}, false)
}

func debugLuaManagedTerminate(state *lua.LState) int {
	client := debugLuaManagedClient(state)
	return client.invoke(state, "client.close", map[string]any{"client_id": client.id, "is_terminate": true}, false)
}
