//go:build scenario

package main

import (
	"math"
	"time"

	"github.com/yuin/gopher-lua"
)

type debugLuaResource struct {
	vm           *debugLuaVM
	kind         string
	initialState lua.LValue
	profileName  string
}

func registerDebugLuaResources(state *lua.LState) {
	serverMeta := state.NewTypeMetatable("ServerHandle")
	state.SetField(serverMeta, "__index", state.NewFunction(debugLuaServerIndex))
	state.SetField(serverMeta, "__metatable", lua.LString("ServerHandle"))
	profileMeta := state.NewTypeMetatable("ProfileHandle")
	state.SetField(profileMeta, "__index", state.NewFunction(debugLuaProfileIndex))
	state.SetField(profileMeta, "__metatable", lua.LString("ProfileHandle"))
}

// Wrap only successful result tables; partial result/error tuples remain intact.
func (e *debugLuaVM) wrapServer(state *lua.LState, count int) int {
	if count != 2 || state.Get(-1) != lua.LNil {
		return count
	}
	snapshot, isTable := state.Get(-2).(*lua.LTable)
	if !isTable {
		return count
	}
	resource := state.NewUserData()
	resource.Value = &debugLuaResource{vm: e, kind: "server", initialState: snapshot}
	state.SetMetatable(resource, state.GetTypeMetatable("ServerHandle"))
	state.Replace(-2, resource)
	return count
}

func (e *debugLuaVM) wrapProfile(state *lua.LState, count int) int {
	request := debugLuaPendingYield(state, count)
	if request != nil {
		request.isProfileResult = true
		return count
	}
	if count != 2 || state.Get(-1) != lua.LNil {
		return count
	}
	snapshot, isTable := state.Get(-2).(*lua.LTable)
	if !isTable {
		return count
	}
	resource, failure := e.profileHandle(state, snapshot)
	if failure != nil {
		return e.returnOperation(state, nil, failure, true)
	}
	state.Replace(-2, resource)
	return count
}

func (e *debugLuaVM) profileHandle(state *lua.LState, snapshot *lua.LTable) (lua.LValue, *debugError) {
	profileName, isName := snapshot.RawGetString("name").(lua.LString)
	if !isName || profileName == "" {
		return lua.LNil, &debugError{Code: "invalid_result", Context: "profileHandle", Message: "profile operation did not return a profile name"}
	}
	resource := state.NewUserData()
	resource.Value = &debugLuaResource{vm: e, kind: "profile", initialState: snapshot, profileName: string(profileName)}
	state.SetMetatable(resource, state.GetTypeMetatable("ProfileHandle"))
	return resource, nil
}

func debugLuaResourceSelf(state *lua.LState, kind string) *debugLuaResource {
	userdata := state.CheckUserData(1)
	resource, isResource := userdata.Value.(*debugLuaResource)
	if !isResource || resource.vm == nil || resource.kind != kind {
		state.ArgError(1, kind+" handle required")
		return nil
	}
	return resource
}

func debugLuaServerIndex(state *lua.LState) int {
	resource := debugLuaResourceSelf(state, "server")
	if resource == nil {
		return 0
	}
	name := state.CheckString(2)
	switch name {
	case "CreateProfile":
		state.Push(state.NewFunction(debugLuaServerCreateProfile))
	case "ResetProfile":
		state.Push(state.NewFunction(debugLuaServerResetProfile))
	case "StartPVP":
		state.Push(state.NewFunction(debugLuaServerStartPVP))
	default:
		state.Push(debugLuaResourceField(resource, name))
	}
	return 1
}

func debugLuaProfileIndex(state *lua.LState) int {
	resource := debugLuaResourceSelf(state, "profile")
	if resource == nil {
		return 0
	}
	name := state.CheckString(2)
	switch name {
	case "Launch":
		state.Push(state.NewFunction(debugLuaProfileLaunch))
	case "Logout":
		state.Push(state.NewFunction(debugLuaProfileLogout))
	case "Snapshot":
		state.Push(state.NewFunction(debugLuaProfileState))
	case "WaitForLogin":
		state.Push(state.NewFunction(debugLuaProfileWaitForLogin))
	case "WaitForReportedLocation":
		state.Push(state.NewFunction(debugLuaProfileWaitForReportedLocation))
	default:
		state.Push(debugLuaResourceField(resource, name))
	}
	return 1
}

func debugLuaProfileLogout(state *lua.LState) int {
	resource := debugLuaResourceSelf(state, "profile")
	if resource == nil {
		return 0
	}
	return resource.vm.invoke(state, "profile.logout", map[string]any{"name": resource.profileName}, false)
}

func debugLuaResourceField(resource *debugLuaResource, name string) lua.LValue {
	snapshot, isTable := resource.initialState.(*lua.LTable)
	if !isTable {
		return lua.LNil
	}
	return snapshot.RawGetString(name)
}

func debugLuaResourceRequest(state *lua.LState, operation string) (map[string]any, *debugError) {
	table, isTable := state.Get(2).(*lua.LTable)
	if !isTable {
		return nil, &debugError{Code: "invalid_request", Context: operation, Message: "request must be an object table"}
	}
	req, err := debugLuaJSON(table, 0)
	if err != nil {
		return nil, &debugError{Code: "invalid_request", Context: operation, Message: err.Error()}
	}
	fields, isObject := req.(map[string]any)
	if !isObject {
		return nil, &debugError{Code: "invalid_request", Context: operation, Message: "request must be an object table with named fields"}
	}
	return fields, nil
}

func debugLuaServerCreateProfile(state *lua.LState) int {
	resource := debugLuaResourceSelf(state, "server")
	if resource == nil {
		return 0
	}
	fields, failure := debugLuaResourceRequest(state, "profile.create")
	if failure != nil {
		return resource.vm.returnOperation(state, nil, failure, true)
	}
	count := resource.vm.invoke(state, "profile.create", fields, true)
	return resource.vm.wrapProfile(state, count)
}

func debugLuaServerResetProfile(state *lua.LState) int {
	resource := debugLuaResourceSelf(state, "server")
	if resource == nil {
		return 0
	}
	fields, failure := debugLuaResourceRequest(state, "profile.reset")
	if failure != nil {
		return resource.vm.returnOperation(state, nil, failure, true)
	}
	count := resource.vm.invoke(state, "profile.reset", fields, true)
	return resource.vm.wrapProfile(state, count)
}

func debugLuaProfileLaunch(state *lua.LState) int {
	resource := debugLuaResourceSelf(state, "profile")
	if resource == nil {
		return 0
	}
	fields, failure := debugLuaResourceRequest(state, "client.launch")
	if failure != nil {
		return resource.vm.returnOperation(state, nil, failure, true)
	}
	for name := range fields {
		if name == "profile_name" {
			return resource.vm.returnOperation(state, nil, &debugError{Code: "invalid_request", Context: "profileLaunch", Message: "profile_name is supplied by the profile handle and cannot be overridden"}, true)
		}
	}
	clientID, isClientID := fields["client_id"].(string)
	if !isClientID || clientID == "" {
		return resource.vm.returnOperation(state, nil, &debugError{Code: "invalid_request", Context: "profileLaunch", Message: "client_id must be a nonempty string"}, true)
	}
	fields["profile_name"] = resource.profileName
	timeoutMS, failure := debugLuaLaunchTimeout(fields)
	if failure != nil {
		return resource.vm.returnOperation(state, nil, failure, true)
	}
	startedAt := time.Now()
	count := resource.vm.invoke(state, "client.launch", fields, true)
	return resource.vm.wrapClient(state, count, clientID, resource.profileName, timeoutMS, startedAt)
}

func debugLuaLaunchTimeout(fields map[string]any) (int, *debugError) {
	timeout, isProvided := fields["timeout_ms"]
	delete(fields, "timeout_ms")
	if !isProvided {
		return 60000, nil
	}
	timeoutMS, isNumber := timeout.(float64)
	if !isNumber || math.IsNaN(timeoutMS) || math.IsInf(timeoutMS, 0) || math.Trunc(timeoutMS) != timeoutMS || timeoutMS < 1 || timeoutMS > 120000 {
		return 0, &debugError{Code: "invalid_request", Context: "clientTimeout", Message: "timeout_ms must be an integer between 1 and 120000"}
	}
	return int(timeoutMS), nil
}

func debugLuaProfileState(state *lua.LState) int {
	resource := debugLuaResourceSelf(state, "profile")
	if resource == nil {
		return 0
	}
	return resource.vm.invoke(state, "profile.state", map[string]any{"name": resource.profileName}, true)
}

func debugLuaProfileWaitForLogin(state *lua.LState) int {
	resource := debugLuaResourceSelf(state, "profile")
	if resource == nil {
		return 0
	}
	return resource.vm.invoke(state, "profile.wait_login", map[string]any{"name": resource.profileName, "timeout_ms": debugLuaTimeoutMilliseconds(state, 2, 0)}, true)
}

func debugLuaProfileWaitForReportedLocation(state *lua.LState) int {
	resource := debugLuaResourceSelf(state, "profile")
	if resource == nil {
		return 0
	}
	revision := float64(state.OptNumber(4, 0))
	if math.IsNaN(revision) || math.IsInf(revision, 0) || revision < 0 || revision > 9007199254740991 || math.Trunc(revision) != revision {
		return resource.vm.returnOperation(state, nil, &debugError{Code: "invalid_request", Context: "presenceRevision", Message: "after revision must be a nonnegative exactly representable integer"}, true)
	}
	return resource.vm.invoke(state, "profile.wait_location", map[string]any{"name": resource.profileName,
		"location": state.CheckString(2), "timeout_ms": debugLuaTimeoutMilliseconds(state, 3, 0), "after_revision": revision}, true)
}
