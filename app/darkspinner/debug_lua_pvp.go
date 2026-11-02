//go:build scenario

package main

import "github.com/yuin/gopher-lua"

func debugLuaPVPIndex(state *lua.LState, name string) int {
	switch name {
	case "Snapshot":
		state.Push(state.NewFunction(debugLuaPVPSnapshot))
	case "Accept":
		state.Push(state.NewFunction(debugLuaPVPAccept))
	case "Wait":
		state.Push(state.NewFunction(debugLuaPVPWait))
	default:
		state.Push(lua.LNil)
	}
	return 1
}

func debugLuaPVPSnapshot(state *lua.LState) int {
	return debugLuaPVPRequest(state, "client.arena_lobby", false)
}

func debugLuaPVPAccept(state *lua.LState) int {
	return debugLuaPVPRequest(state, "client.accept_arena", true)
}

func debugLuaPVPWait(state *lua.LState) int {
	return debugLuaPVPRequest(state, "client.wait_arena", true)
}

func debugLuaPVPRequest(state *lua.LState, operation string, isTimed bool) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	if !isTimed {
		return scope.client.invoke(state, operation, map[string]any{"client_id": scope.client.id}, true)
	}
	timeoutMS := debugLuaClientTimeout(state, scope.client, 2)
	return scope.client.vm.invokeTimed(state, operation, map[string]any{
		"client_id": scope.client.id, "timeout_ms": timeoutMS,
	}, true, timeoutMS)
}

func debugLuaServerStartPVP(state *lua.LState) int {
	resource := debugLuaResourceSelf(state, "server")
	if resource == nil {
		return 0
	}
	options := state.CheckTable(2)
	teams, areTeams := options.RawGetString("teams").(*lua.LTable)
	if !areTeams || teams.Len() != 2 {
		state.ArgError(2, "teams must contain exactly two equally sized lists of managed clients")
		return 0
	}
	clientTeams := make([][]string, 0, 2)
	seenClientIDs := make(map[string]bool)
	for teamIndex := 1; teamIndex <= 2; teamIndex++ {
		team, isTeam := teams.RawGetInt(teamIndex).(*lua.LTable)
		if !isTeam || team.Len() < 1 || team.Len() > 2 {
			state.ArgError(2, "each PvP team requires one or two managed clients")
			return 0
		}
		clientIDs := make([]string, 0, team.Len())
		for memberIndex := 1; memberIndex <= team.Len(); memberIndex++ {
			userdata, isUserdata := team.RawGetInt(memberIndex).(*lua.LUserData)
			if !isUserdata {
				state.ArgError(2, "PvP participants must be managed clients")
				return 0
			}
			client, isClient := userdata.Value.(*debugLuaClient)
			if !isClient || client == nil || client.vm != resource.vm || seenClientIDs[client.id] {
				state.ArgError(2, "PvP participants must be distinct clients owned by this scenario")
				return 0
			}
			seenClientIDs[client.id] = true
			clientIDs = append(clientIDs, client.id)
		}
		clientTeams = append(clientTeams, clientIDs)
	}
	if len(clientTeams[0]) != len(clientTeams[1]) {
		state.ArgError(2, "PvP teams must have the same number of participants")
		return 0
	}
	return resource.vm.invoke(state, "pvp.start", map[string]any{"teams": clientTeams}, true)
}
