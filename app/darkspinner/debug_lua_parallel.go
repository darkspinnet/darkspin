//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/yuin/gopher-lua"
)

type debugLuaParallel struct {
	vm              *debugLuaVM
	owner           *lua.LState
	branches        []*debugLuaBranch
	branchesByState map[*lua.LState]*debugLuaBranch
	pendingBranches map[uint64]*debugLuaBranch
}

type debugLuaBranch struct {
	name    string
	state   *lua.LState
	entry   *lua.LFunction
	cancel  context.CancelFunc
	request *debugLuaYield
	failure *debugLuaFailure
}

type debugLuaYield struct {
	request         debugOperationRequest
	isResult        bool
	isProfileResult bool
	client          *debugLuaClient
	npcClient       *debugLuaClient
}

func debugLuaPendingYield(state *lua.LState, count int) *debugLuaYield {
	if count != -1 {
		return nil
	}
	userdata, isUserdata := state.Get(-1).(*lua.LUserData)
	if !isUserdata {
		return nil
	}
	request, isRequest := userdata.Value.(*debugLuaYield)
	if !isRequest {
		return nil
	}
	return request
}

func (e *debugLuaVM) runParallel(state *lua.LState) int {
	if !e.isRunning || e.parallel != nil {
		return e.returnOperation(state, nil, &debugError{Code: "conflict", Context: "parallel", Message: "Parallel is available only from the entry function and cannot be nested"}, false)
	}
	group, err := e.newParallel(state)
	if err != nil {
		return e.returnOperation(state, nil, &debugError{Code: "invalid_request", Context: "parallel", Message: err.Error()}, false)
	}
	e.parallel = group
	defer e.finishParallel(group)
	failure := group.run()
	if failure != nil {
		state.Push(debugLuaErrorValue(state, failure))
	} else {
		state.Push(lua.LNil)
	}
	return 1
}

func (e *debugLuaVM) finishParallel(group *debugLuaParallel) {
	e.parallel = nil
	for _, branch := range group.branches {
		if branch.cancel != nil {
			branch.cancel()
		}
		branch.state.Close()
	}
}

func (e *debugLuaVM) newParallel(state *lua.LState) (*debugLuaParallel, error) {
	functions := state.CheckTable(1)
	entries := make(map[string]*lua.LFunction)
	key := lua.LValue(lua.LNil)
	for {
		name, entry := functions.Next(key)
		if name == lua.LNil {
			break
		}
		key = name
		branchName, isName := name.(lua.LString)
		branchEntry, isFunction := entry.(*lua.LFunction)
		if !isName || branchName == "" || len(branchName) > 64 || !isFunction {
			return nil, errors.New("Parallel requires named branch functions with names of 1 to 64 bytes")
		}
		entries[string(branchName)] = branchEntry
	}
	if len(entries) == 0 || len(entries) > debugLuaPendingLimit {
		return nil, errors.New("Parallel requires between one and eight named branches")
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	group := &debugLuaParallel{vm: e, owner: state, branchesByState: make(map[*lua.LState]*debugLuaBranch), pendingBranches: make(map[uint64]*debugLuaBranch)}
	for _, name := range names {
		thread, cancel := state.NewThread()
		branch := &debugLuaBranch{name: name, state: thread, cancel: cancel, entry: entries[name]}
		group.branches = append(group.branches, branch)
		group.branchesByState[thread] = branch
	}
	return group, nil
}

func (e *debugLuaParallel) yieldOperation(state *lua.LState, operation string, payload json.RawMessage, isResult bool, timeoutMS int) int {
	if e.branchesByState[state] == nil || (!isIndependentDebugOperation(operation) && operation != "status") {
		return e.vm.returnOperation(state, nil, &debugError{Code: "unsupported_operation", Context: "parallel", Message: "Parallel supports independent client operations, profile creation and client launch/close; server startup and profile reset must remain outside the block"}, isResult)
	}
	request := state.NewUserData()
	request.Value = &debugLuaYield{request: debugOperationRequest{Operation: operation, Payload: payload, TimeoutMS: uint32(timeoutMS)}, isResult: isResult}
	return state.Yield(request)
}

func (e *debugLuaParallel) run() *debugLuaFailure {
	for _, branch := range e.branches {
		err := e.resume(branch, nil)
		if err != nil {
			return debugLuaGoFailure("parallelStart", err)
		}
	}
	for len(e.pendingBranches) > 0 {
		frame, err := e.vm.readFrame()
		if err != nil {
			return debugLuaGoFailure("parallelReceive", err)
		}
		branch := e.pendingBranches[frame.Sequence]
		if frame.Kind != "result" || branch == nil {
			return debugLuaGoFailure("parallelSequence", errors.New("unexpected parallel operation receipt"))
		}
		delete(e.pendingBranches, frame.Sequence)
		var result any
		if len(frame.Result) > 0 {
			err = json.Unmarshal(frame.Result, &result)
			if err != nil {
				return debugLuaGoFailure("parallelResult", err)
			}
		}
		args := make([]lua.LValue, 0, 2)
		if branch.request.isResult {
			response := debugLuaValue(branch.state, result)
			if frame.Failure == nil && (branch.request.isProfileResult || branch.request.client != nil || branch.request.npcClient != nil) {
				var failure *debugError
				response, failure = e.wrapResult(branch.state, branch.request, response)
				if failure != nil {
					frame.Failure = failure
				}
			}
			args = append(args, response)
		}
		if frame.Failure != nil {
			args = append(args, debugLuaErrorValue(branch.state, &debugLuaFailure{Failure: frame.Failure}))
		} else {
			args = append(args, lua.LNil)
		}
		err = e.resume(branch, args)
		if err != nil {
			return debugLuaGoFailure("parallelResume", err)
		}
	}
	failures := make(map[string]*debugError)
	traces := make([]string, 0, len(e.branches))
	for _, branch := range e.branches {
		if branch.failure == nil {
			continue
		}
		failures[branch.name] = branch.failure.Failure
		if branch.failure.Traceback != "" {
			traces = append(traces, branch.name+":\n"+branch.failure.Traceback)
		}
	}
	if len(failures) == 0 {
		return nil
	}
	return &debugLuaFailure{Failure: &debugError{Code: "parallel_failed", Context: "parallel", Message: fmt.Sprintf("%d branch(es) failed", len(failures)), Failures: failures}, Traceback: strings.Join(traces, "\n")}
}

func (e *debugLuaParallel) wrapResult(state *lua.LState, request *debugLuaYield, response lua.LValue) (lua.LValue, *debugError) {
	snapshot, isTable := response.(*lua.LTable)
	if !isTable {
		return lua.LNil, &debugError{Code: "invalid_result", Context: "parallelHandle", Message: "profile/client operation did not return a snapshot"}
	}
	if request.isProfileResult {
		return e.vm.profileHandle(state, snapshot)
	}
	if request.npcClient != nil {
		return e.vm.npcHandle(state, request.npcClient, snapshot)
	}
	return e.vm.clientHandle(state, request.client, snapshot)
}

func (e *debugLuaParallel) resume(branch *debugLuaBranch, args []lua.LValue) error {
	status, err, results := e.owner.Resume(branch.state, branch.entry, args...)
	if err != nil {
		branch.failure = debugLuaCallFailure("parallelBranch", err)
		if branch.failure.Traceback == "" {
			// Resume removes the failing Go error frame but retains its Lua
			// caller stack. Capture that branch before finishParallel closes it.
			branch.failure.Traceback = debugLuaTracebackFrom(branch.state, 0)
		}
		return nil
	}
	if status == lua.ResumeOK {
		if len(results) > 0 && results[0] != lua.LNil {
			branch.failure = &debugLuaFailure{Failure: debugLuaCause(results[0])}
		}
		return nil
	}
	if status != lua.ResumeYield || len(results) != 1 {
		return errors.New("branch returned an unsupported coroutine state")
	}
	requestData, isUserdata := results[0].(*lua.LUserData)
	if !isUserdata {
		return errors.New("branch yielded an unsupported request")
	}
	request, isRequest := requestData.Value.(*debugLuaYield)
	if !isRequest {
		return errors.New("branch yielded an unsupported request type")
	}
	branch.request = request
	e.vm.sequence++
	e.pendingBranches[e.vm.sequence] = branch
	err = e.vm.encoder.Encode(debugLuaFrame{Version: 1, Kind: "operation", Sequence: e.vm.sequence, Request: &request.request})
	if err != nil {
		return fmt.Errorf("parallelSend: %w", err)
	}
	return nil
}
