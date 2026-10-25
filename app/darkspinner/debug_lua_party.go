//go:build scenario

package main

import "github.com/yuin/gopher-lua"

func debugLuaPartyIndex(state *lua.LState, name string) int {
	switch name {
	case "Create":
		state.Push(state.NewFunction(debugLuaPartyCreate))
	case "Invite":
		state.Push(state.NewFunction(debugLuaPartyInvite))
	case "Join":
		state.Push(state.NewFunction(debugLuaPartyJoin))
	case "Snapshot":
		state.Push(state.NewFunction(debugLuaPartyState))
	default:
		state.Push(lua.LNil)
	}
	return 1
}

func debugLuaPartyCreate(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "party.create", map[string]any{
		"client_id": scope.client.id, "name": state.OptString(2, "Scenario"),
	}, true)
}

func debugLuaPartyState(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "party.state", map[string]any{"client_id": scope.client.id}, true)
}

func debugLuaPartyInvite(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	target := debugLuaPartyTarget(state, scope.client)
	return scope.client.invoke(state, "party.invite", map[string]any{
		"client_id": scope.client.id, "invitee_client_id": target.id,
	}, true)
}

func debugLuaPartyJoin(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	leader := debugLuaPartyTarget(state, scope.client)
	return scope.client.invoke(state, "party.join", map[string]any{
		"client_id": scope.client.id, "leader_client_id": leader.id,
	}, true)
}

func debugLuaPartyTarget(state *lua.LState, client *debugLuaClient) *debugLuaClient {
	userdata := state.CheckUserData(2)
	target, isClient := userdata.Value.(*debugLuaClient)
	if !isClient || target == nil || target.vm != client.vm || target.id == client.id {
		state.ArgError(2, "a different managed client from this scenario is required")
		return nil
	}
	return target
}
