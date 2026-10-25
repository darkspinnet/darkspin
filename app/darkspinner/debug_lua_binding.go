//go:build scenario

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/yuin/gopher-lua"
)

func (e *debugLuaVM) register(state *lua.LState) {
	registerDebugLuaResources(state)
	registerDebugLuaClientScopes(state)
	registerDebugLuaNPC(state)
	registerDebugLuaScreen(state)
	errorMeta := state.NewTypeMetatable("ScenarioError")
	state.SetField(errorMeta, "__tostring", state.NewFunction(debugLuaErrorString))
	state.SetField(errorMeta, "__index", state.NewFunction(debugLuaErrorIndex))
	state.SetField(errorMeta, "__metatable", lua.LString("ScenarioError"))
	clientMeta := state.NewTypeMetatable("ManagedClient")
	state.SetField(clientMeta, "__index", state.NewFunction(debugLuaClientIndex))
	state.SetField(clientMeta, "__metatable", lua.LString("ManagedClient"))
	namespace := state.NewTable()
	state.SetFuncs(namespace, map[string]lua.LGFunction{
		"Check": debugLuaCheck, "Must": debugLuaMust, "RequireAPI": debugLuaRequireAPI,
		"Parallel": e.runParallel,
		"Sleep":    e.sleep,
		"Time":     e.elapsedTime,
		"Snapshot": e.status, "Status": e.status, "StartServer": e.startServer, "CreateProfile": e.createProfile,
		"ResetProfile": e.resetProfile, "ClientWindow": e.clientWindow,
		"Click":           e.click,
		"SnapshotProfile": e.profileState, "WaitForReportedLocation": e.waitForReportedLocation,
		"LaunchClient": e.launchClient, "SnapshotClient": e.clientState, "CloseClient": e.closeClient,
		"WaitForLogin": e.waitForLogin,
		"Screenshot":   e.screenshot,
	})
	state.SetGlobal("scenario", namespace)
}

func (e *debugLuaVM) sleep(state *lua.LState) int {
	return e.invoke(state, "scenario.sleep", map[string]any{"duration_ms": debugLuaTimeoutMilliseconds(state, 1, 0)}, false)
}

func (e *debugLuaVM) elapsedTime(state *lua.LState) int {
	state.Push(lua.LNumber(float64(time.Since(e.startedAt)) / float64(time.Millisecond)))
	return 1
}

func debugLuaErrorValue(state *lua.LState, failure *debugLuaFailure) *lua.LUserData {
	userdata := state.NewUserData()
	userdata.Value = failure
	state.SetMetatable(userdata, state.GetTypeMetatable("ScenarioError"))
	return userdata
}

func debugLuaCause(cause lua.LValue) *debugError {
	userdata, isUserdata := cause.(*lua.LUserData)
	if isUserdata {
		failure, isFailure := userdata.Value.(*debugLuaFailure)
		if isFailure {
			return failure.Failure
		}
	}
	return &debugError{Code: "script_failed", Message: cause.String()}
}

func debugLuaErrorString(state *lua.LState) int {
	state.Push(lua.LString(debugErrorDomain(debugLuaCause(state.Get(1))).Error()))
	return 1
}

func debugLuaErrorIndex(state *lua.LState) int {
	failure := debugLuaCause(state.Get(1))
	switch state.CheckString(2) {
	case "Code":
		state.Push(lua.LString(failure.Code))
	case "Context":
		state.Push(lua.LString(failure.Context))
	case "Message":
		state.Push(lua.LString(failure.Message))
	case "ClientInstanceID":
		state.Push(lua.LString(failure.ClientInstanceID))
	case "ActionID":
		state.Push(lua.LString(failure.ActionID))
	case "Details":
		state.Push(debugLuaValue(state, failure.Details))
	case "Cause":
		if failure.Cause == nil {
			state.Push(lua.LNil)
		} else {
			state.Push(debugLuaErrorValue(state, &debugLuaFailure{Failure: failure.Cause}))
		}
	case "Failures":
		if len(failure.Failures) == 0 {
			state.Push(lua.LNil)
		} else {
			failures := state.NewTable()
			for name, branchFailure := range failure.Failures {
				failures.RawSetString(name, debugLuaErrorValue(state, &debugLuaFailure{Failure: branchFailure}))
			}
			state.Push(failures)
		}
	default:
		state.Push(lua.LNil)
	}
	return 1
}

func debugLuaCheck(state *lua.LState) int {
	if state.GetTop() != 1 {
		state.RaiseError("Check expects only an error result; context labels are no longer accepted")
		return 0
	}
	if state.Get(1) == lua.LNil {
		return 0
	}
	debugLuaRaise(state, state.Get(1))
	return 0
}

func debugLuaMust(state *lua.LState) int {
	if state.GetTop() != 2 {
		state.RaiseError("Must expects a result and error; context labels are no longer accepted")
		return 0
	}
	if state.Get(2) != lua.LNil {
		debugLuaRaise(state, state.Get(2))
		return 0
	}
	if state.Get(1) == lua.LNil {
		debugLuaRaise(state, debugLuaErrorValue(state, &debugLuaFailure{Failure: &debugError{Code: "missing_result", Message: "operation returned no required result"}}))
		return 0
	}
	state.Push(state.Get(1))
	return 1
}

func debugLuaRaise(state *lua.LState, cause lua.LValue) {
	failure := &debugLuaFailure{Failure: debugLuaCause(cause),
		Traceback: debugLuaTraceback(state)}
	state.Error(debugLuaErrorValue(state, failure), 0)
}

func debugLuaRequireAPI(state *lua.LState) int {
	if state.CheckInt(1) == 1 {
		state.Push(lua.LNil)
	} else {
		state.Push(debugLuaErrorValue(state, &debugLuaFailure{Failure: &debugError{
			Code: "unavailable", Context: "apiVersion", Message: "only scenario API version 1 is supported"}}))
	}
	return 1
}

func (e *debugLuaVM) status(state *lua.LState) int {
	return e.invoke(state, "status", map[string]any{}, true)
}
func (e *debugLuaVM) startServer(state *lua.LState) int {
	return e.wrapServer(state, e.invokeTable(state, "server.start"))
}
func (e *debugLuaVM) createProfile(state *lua.LState) int {
	return e.wrapProfile(state, e.invokeTable(state, "profile.create"))
}
func (e *debugLuaVM) resetProfile(state *lua.LState) int {
	return e.wrapProfile(state, e.invokeTable(state, "profile.reset"))
}
func (e *debugLuaVM) clientWindow(state *lua.LState) int {
	return e.invoke(state, "client.window", map[string]any{"client_id": state.CheckString(1)}, true)
}
func (e *debugLuaVM) click(state *lua.LState) int {
	return e.invoke(state, "client.click", map[string]any{"client_id": state.CheckString(1), "x": state.CheckInt(2), "y": state.CheckInt(3), "button": "left"}, true)
}
func (e *debugLuaVM) profileState(state *lua.LState) int {
	return e.invoke(state, "profile.state", map[string]any{"name": state.CheckString(1)}, true)
}
func (e *debugLuaVM) waitForReportedLocation(state *lua.LState) int {
	revision := float64(state.OptNumber(4, 0))
	if math.IsNaN(revision) || math.IsInf(revision, 0) || revision < 0 || revision > 9007199254740991 || math.Trunc(revision) != revision {
		return e.returnOperation(state, nil, &debugError{Code: "invalid_request", Context: "presenceRevision", Message: "after revision must be a nonnegative exactly representable integer"}, true)
	}
	return e.invoke(state, "profile.wait_location", map[string]any{"name": state.CheckString(1), "location": state.CheckString(2), "timeout_ms": debugLuaTimeoutMilliseconds(state, 3, 0), "after_revision": revision}, true)
}
func (e *debugLuaVM) launchClient(state *lua.LState) int {
	req := state.CheckTable(1)
	clientID := req.RawGetString("client_id").String()
	profileName := req.RawGetString("profile_name").String()
	fields, err := debugLuaJSON(req, 0)
	if err != nil {
		return e.returnOperation(state, nil, &debugError{Code: "invalid_request", Context: "launchRequest", Message: err.Error()}, true)
	}
	options, isObject := fields.(map[string]any)
	if !isObject {
		return e.returnOperation(state, nil, &debugError{Code: "invalid_request", Context: "launchRequest", Message: "launch requires named fields"}, true)
	}
	timeoutMS, failure := debugLuaLaunchTimeout(options)
	if failure != nil {
		return e.returnOperation(state, nil, failure, true)
	}
	startedAt := time.Now()
	count := e.invoke(state, "client.launch", options, true)
	return e.wrapClient(state, count, clientID, profileName, timeoutMS, startedAt)
}

func (e *debugLuaVM) wrapClient(state *lua.LState, count int, clientID, profileName string, timeoutMS int, startedAt time.Time) int {
	if count != 2 {
		return count
	}
	initialState, isTable := state.Get(-2).(*lua.LTable)
	if state.Get(-1) == lua.LNil && isTable {
		client := state.NewUserData()
		client.Value = &debugLuaClient{vm: e, id: clientID, profileName: profileName, timeoutMS: timeoutMS, startedAt: startedAt, initialState: initialState}
		state.SetMetatable(client, state.GetTypeMetatable("ManagedClient"))
		state.Replace(-2, client)
	}
	return count
}
func (e *debugLuaVM) clientState(state *lua.LState) int {
	return e.invoke(state, "client.state", map[string]any{"client_id": state.CheckString(1)}, true)
}
func (e *debugLuaVM) closeClient(state *lua.LState) int {
	return e.invoke(state, "client.close", map[string]any{"client_id": state.CheckString(1)}, false)
}

func (e *debugLuaVM) waitForLogin(state *lua.LState) int {
	return e.invoke(state, "profile.wait_login", map[string]any{"name": state.CheckString(1), "timeout_ms": debugLuaTimeoutMilliseconds(state, 2, 0)}, true)
}

func (e *debugLuaVM) screenshot(state *lua.LState) int {
	return e.invoke(state, "client.screenshot", map[string]any{"client_id": state.CheckString(1), "label": state.CheckString(2)}, true)
}

func (e *debugLuaVM) invokeTable(state *lua.LState, operation string) int {
	req, err := debugLuaJSON(state.CheckTable(1), 0)
	if err != nil {
		return e.returnOperation(state, nil, &debugError{Code: "invalid_request", Context: operation, Message: err.Error()}, true)
	}
	return e.invoke(state, operation, req, true)
}

func (e *debugLuaVM) invoke(state *lua.LState, operation string, req any, isResult bool) int {
	return e.invokeTimed(state, operation, req, isResult, 0)
}

func (e *debugLuaVM) invokeTimed(state *lua.LState, operation string, req any, isResult bool, timeoutMS int) int {
	if !e.isRunning {
		return e.returnOperation(state, nil, &debugError{Code: "conflict", Context: operation,
			Message: "scenario operations are disabled while evaluating the source chunk; call them from run()"}, isResult)
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return e.returnOperation(state, nil, &debugError{Code: "invalid_request", Context: operation, Message: err.Error()}, isResult)
	}
	if e.parallel != nil {
		return e.parallel.yieldOperation(state, operation, payload, isResult, timeoutMS)
	}
	e.sequence++
	err = e.encoder.Encode(debugLuaFrame{Version: 1, Kind: "operation", Sequence: e.sequence,
		Request: &debugOperationRequest{Operation: operation, Payload: payload, TimeoutMS: uint32(timeoutMS)}})
	if err != nil {
		return e.returnOperation(state, nil, &debugError{Code: "disconnected", Context: "luaSend", Message: err.Error()}, isResult)
	}
	response, err := e.readFrame()
	if err != nil {
		return e.returnOperation(state, nil, &debugError{Code: "disconnected", Context: "luaReceive", Message: err.Error()}, isResult)
	}
	if response.Kind != "result" || response.Sequence != e.sequence {
		return e.returnOperation(state, nil, &debugError{Code: "invalid_request", Context: "luaSequence", Message: "unexpected helper response identity"}, isResult)
	}
	var result any
	if len(response.Result) != 0 {
		err = json.Unmarshal(response.Result, &result)
		if err != nil {
			return e.returnOperation(state, nil, &debugError{Code: "invalid_request", Context: "luaResult", Message: err.Error()}, isResult)
		}
	}
	return e.returnOperation(state, result, response.Failure, isResult)
}

func (e *debugLuaVM) returnOperation(state *lua.LState, result any, failure *debugError, isResult bool) int {
	if isResult {
		state.Push(debugLuaValue(state, result))
	}
	if failure == nil {
		state.Push(lua.LNil)
	} else {
		state.Push(debugLuaErrorValue(state, &debugLuaFailure{Failure: failure}))
	}
	if isResult {
		return 2
	}
	return 1
}

func debugLuaValue(state *lua.LState, result any) lua.LValue {
	switch item := result.(type) {
	case nil:
		return lua.LNil
	case bool:
		return lua.LBool(item)
	case string:
		return lua.LString(item)
	case float64:
		return lua.LNumber(item)
	case []any:
		table := state.NewTable()
		for _, element := range item {
			table.Append(debugLuaValue(state, element))
		}
		return table
	case map[string]any:
		table := state.NewTable()
		for name, element := range item {
			table.RawSetString(name, debugLuaValue(state, element))
		}
		return table
	default:
		return lua.LNil
	}
}

func debugLuaJSON(item lua.LValue, depth int) (any, error) {
	if depth > 16 {
		return nil, errors.New("request table nesting exceeds 16 levels")
	}
	switch scalar := item.(type) {
	case *lua.LTable:
		if scalar.Len() > 0 {
			if scalar.Len() > 128 {
				return nil, errors.New("request array exceeds 128 elements")
			}
			elements := make([]any, scalar.Len())
			key := lua.LValue(lua.LNil)
			count := 0
			for {
				nextKey, element := scalar.Next(key)
				if nextKey == lua.LNil {
					break
				}
				key = nextKey
				index, isNumber := key.(lua.LNumber)
				if !isNumber || index != lua.LNumber(int(index)) || index < 1 || int(index) > len(elements) {
					return nil, errors.New("request arrays require consecutive positive integer keys")
				}
				converted, err := debugLuaJSON(element, depth+1)
				if err != nil {
					return nil, fmt.Errorf("requestElement: %w", err)
				}
				elements[int(index)-1] = converted
				count++
				if count > 128 {
					return nil, errors.New("request array exceeds 128 elements")
				}
			}
			return elements, nil
		}
		fields := make(map[string]any)
		key := lua.LValue(lua.LNil)
		for {
			nextKey, element := scalar.Next(key)
			if nextKey == lua.LNil {
				break
			}
			key = nextKey
			name, isString := key.(lua.LString)
			if !isString {
				return nil, errors.New("request tables require string field names")
			}
			if len(fields) >= 128 {
				return nil, errors.New("request table exceeds 128 fields")
			}
			converted, err := debugLuaJSON(element, depth+1)
			if err != nil {
				return nil, fmt.Errorf("requestField: %w", err)
			}
			fields[string(name)] = converted
		}
		return fields, nil
	case lua.LString:
		return string(scalar), nil
	case lua.LBool:
		return bool(scalar), nil
	case lua.LNumber:
		if math.IsInf(float64(scalar), 0) || math.IsNaN(float64(scalar)) {
			return nil, errors.New("request numbers must be finite")
		}
		return float64(scalar), nil
	case *lua.LNilType:
		return nil, nil
	default:
		return nil, fmt.Errorf("request cannot encode Lua %s", strings.ToLower(item.Type().String()))
	}
}
