//go:build scenario

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yuin/gopher-lua"
)

type debugLuaVM struct {
	scanner   *bufio.Scanner
	encoder   *json.Encoder
	sequence  uint64
	isRunning bool
	parallel  *debugLuaParallel
	startedAt time.Time
}

type debugLuaFailure struct {
	Failure   *debugError
	Traceback string
}

func runDebugLuaWorker(arguments []string) (bool, error) {
	if len(arguments) != 1 || arguments[0] != debugLuaWorkerArgument {
		return false, nil
	}
	for _, r := range []*os.File{os.Stdin, os.Stdout} {
		fi, err := r.Stat()
		if err != nil {
			return true, fmt.Errorf("luaPipeStat: %w", err)
		}
		if fi.Mode()&os.ModeNamedPipe == 0 {
			return true, errors.New("Lua helper requires inherited private pipes")
		}
	}
	vm := &debugLuaVM{scanner: bufio.NewScanner(os.Stdin), encoder: json.NewEncoder(os.Stdout)}
	vm.scanner.Buffer(make([]byte, 4096), debugLuaFrameLimit)
	frame, err := vm.readFrame()
	if err != nil {
		return true, fmt.Errorf("luaBootstrap: %w", err)
	}
	if frame.Kind != "bootstrap" || frame.Sequence != 0 || frame.Bootstrap == nil || len(frame.Bootstrap.Source) > debugScriptLimit {
		return true, errors.New("Lua helper requires a bounded source bootstrap")
	}
	deadline := frame.Bootstrap.Deadline
	if deadline.IsZero() || time.Until(deadline) <= 0 || time.Until(deadline) > debugLuaMaximumExecutionLimit+time.Second {
		return true, errors.New("Lua helper deadline is outside the execution limit")
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	failure := vm.execute(ctx, *frame.Bootstrap)
	if ctx.Err() != nil {
		deadlineFailure := &debugLuaFailure{Failure: &debugError{Code: "deadline_exceeded", Context: "luaDeadline", Message: ctx.Err().Error()}}
		if failure != nil {
			deadlineFailure.Failure.Cause = failure.Failure
			deadlineFailure.Traceback = failure.Traceback
		}
		failure = deadlineFailure
	}
	response := debugLuaFrame{Version: 1, Kind: "finished", Sequence: vm.sequence}
	if failure != nil {
		response.Failure = failure.Failure
		response.Traceback = failure.Traceback
	}
	err = vm.encoder.Encode(response)
	if err != nil {
		return true, fmt.Errorf("luaFinish: %w", err)
	}
	return true, nil
}

func (e *debugLuaVM) readFrame() (debugLuaFrame, error) {
	if !e.scanner.Scan() {
		err := e.scanner.Err()
		if err != nil {
			return debugLuaFrame{}, fmt.Errorf("luaPipeRead: %w", err)
		}
		return debugLuaFrame{}, errors.New("Lua helper pipe closed")
	}
	var frame debugLuaFrame
	err := decodeDebugJSON(e.scanner.Bytes(), &frame)
	if err != nil {
		return frame, fmt.Errorf("luaFrameDecode: %w", err)
	}
	if frame.Version != 1 {
		return frame, errors.New("unsupported Lua helper protocol")
	}
	return frame, nil
}

func (e *debugLuaVM) execute(ctx context.Context, bootstrap debugLuaBootstrap) *debugLuaFailure {
	e.startedAt = time.Now()
	state := lua.NewState(lua.Options{SkipOpenLibs: true, CallStackSize: 256,
		RegistrySize: 1024, RegistryMaxSize: 32768, RegistryGrowStep: 1024})
	defer state.Close()
	state.SetContext(ctx)
	for _, library := range []lua.LGFunction{lua.OpenBase, lua.OpenString, lua.OpenTable, lua.OpenMath} {
		state.Push(state.NewFunction(library))
		err := state.PCall(0, 0, nil)
		if err != nil {
			return debugLuaGoFailure("luaLibrary", err)
		}
	}
	for _, name := range []string{"dofile", "load", "loadfile", "loadstring", "module", "require", "print", "_printregs", "newproxy", "collectgarbage"} {
		state.SetGlobal(name, lua.LNil)
	}
	e.register(state)
	chunk, err := state.Load(strings.NewReader(bootstrap.Source), "@"+bootstrap.Name)
	if err != nil {
		return debugLuaGoFailure("luaCompile", err)
	}
	handler := state.NewFunction(debugLuaErrorHandler)
	err = state.CallByParam(lua.P{Fn: chunk, NRet: 1, Protect: true, Handler: handler})
	if err != nil {
		return debugLuaCallFailure("luaChunk", err)
	}
	run, isFunction := state.Get(-1).(*lua.LFunction)
	state.Pop(1)
	if !isFunction {
		// Preserve existing developer-owned scripts while shipped starters use a
		// returned local function and require no undeclared lowercase global.
		run, isFunction = state.GetGlobal("run").(*lua.LFunction)
	}
	if !isFunction {
		return debugLuaGoFailure("luaEntry", errors.New("script must return its entry function"))
	}
	e.isRunning = true
	err = state.CallByParam(lua.P{Fn: run, NRet: 1, Protect: true, Handler: handler})
	if err != nil {
		return debugLuaCallFailure("luaRun", err)
	}
	if state.Get(-1) != lua.LNil {
		return &debugLuaFailure{Failure: &debugError{Code: "script_failed", Context: "luaReturn",
			Cause: debugLuaCause(state.Get(-1))}}
	}
	return nil
}

func debugLuaGoFailure(step string, err error) *debugLuaFailure {
	return &debugLuaFailure{Failure: &debugError{Code: "script_failed", Context: step, Message: err.Error()}}
}

func debugLuaCallFailure(step string, err error) *debugLuaFailure {
	apiErr, isAPI := err.(*lua.ApiError)
	if isAPI {
		userdata, isUserdata := apiErr.Object.(*lua.LUserData)
		if isUserdata {
			failure, isFailure := userdata.Value.(*debugLuaFailure)
			if isFailure {
				return failure
			}
		}
		return &debugLuaFailure{Failure: &debugError{Code: "script_failed", Context: step,
			Message: apiErr.Object.String()}, Traceback: apiErr.StackTrace}
	}
	return debugLuaGoFailure(step, err)
}

func debugLuaTraceback(state *lua.LState) string {
	return debugLuaTracebackFrom(state, 1)
}

func debugLuaTracebackFrom(state *lua.LState, firstLevel int) string {
	lines := []string{"stack traceback:"}
	for level := firstLevel; level < 64; level++ {
		frame, isPresent := state.GetStack(level)
		if !isPresent {
			break
		}
		function, err := state.GetInfo("Sln", frame, lua.LNil)
		if err != nil {
			lines = append(lines, "  <frame unavailable: "+err.Error()+">")
			continue
		}
		if function != lua.LNil {
			// GetInfo only returns a function when explicitly requested with 'f'.
		}
		lines = append(lines, fmt.Sprintf("  %s:%d in %s", frame.Source, frame.CurrentLine, frame.Name))
	}
	return strings.Join(lines, "\n")
}

func debugLuaErrorHandler(state *lua.LState) int {
	userdata, isUserdata := state.Get(1).(*lua.LUserData)
	if isUserdata {
		failure, isFailure := userdata.Value.(*debugLuaFailure)
		if isFailure && failure.Traceback != "" {
			state.Push(userdata)
			return 1
		}
	}
	failure := &debugLuaFailure{Failure: debugLuaCause(state.Get(1)), Traceback: debugLuaTraceback(state)}
	state.Push(debugLuaErrorValue(state, failure))
	return 1
}
