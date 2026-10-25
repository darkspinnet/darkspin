//go:build scenario

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

type debugLuaHost struct {
	supervisor *debugSupervisor
	scanner    *bufio.Scanner
	encoder    *json.Encoder
	sequence   uint64
	input      io.WriteCloser
	output     io.ReadCloser
	closeOnce  sync.Once
}

const debugLuaPendingLimit = 8

type debugLuaIncoming struct {
	frame debugLuaFrame
	err   error
}

type debugLuaCompletion struct {
	sequence  uint64
	operation string
	result    any
	err       error
}

func (e *debugSupervisor) executeLua(ctx context.Context, script debugScript, source []byte) *debugLuaFailure {
	executable, err := os.Executable()
	if err != nil {
		return debugLuaGoFailure("helperExecutable", err)
	}
	limit := e.executionLimit
	if limit == 0 {
		limit = debugLuaExecutionLimit
	}
	deadline := time.Now().Add(limit)
	helperContext, cancel := context.WithDeadline(ctx, deadline.Add(5*time.Second))
	defer cancel()
	command := exec.CommandContext(helperContext, executable, debugLuaWorkerArgument)
	configureDebugLuaProcess(command)
	command.WaitDelay = 2 * time.Second
	w, err := os.CreateTemp(e.reportDirectory, "lua-helper-*.log")
	if err != nil {
		return debugLuaGoFailure("helperLog", err)
	}
	defer e.closeLuaPipe(w)
	command.Stderr = w
	input, err := command.StdinPipe()
	if err != nil {
		return debugLuaGoFailure("helperInput", err)
	}
	defer e.closeLuaPipe(input)
	output, err := command.StdoutPipe()
	if err != nil {
		return debugLuaGoFailure("helperOutput", err)
	}
	defer e.closeLuaPipe(output)
	err = command.Start()
	if err != nil {
		return debugLuaGoFailure("helperStart", err)
	}
	job, err := containDebugLuaProcess(command.Process)
	if err != nil {
		killErr := command.Process.Kill()
		waitErr := command.Wait()
		return debugLuaGoFailure("helperContain", errors.Join(err, killErr, waitErr))
	}
	defer e.closeLuaJob(job)
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), debugLuaFrameLimit)
	host := &debugLuaHost{supervisor: e, scanner: scanner, encoder: json.NewEncoder(input), input: input, output: output}
	err = host.encoder.Encode(debugLuaFrame{Version: 1, Kind: "bootstrap", Bootstrap: &debugLuaBootstrap{
		Name: script.Name, Source: string(source), Deadline: deadline}})
	if err != nil {
		cancel()
		waitErr := command.Wait()
		return debugLuaGoFailure("helperBootstrap", errors.Join(err, waitErr))
	}
	// This owner writes responses; bounded operation workers own no Lua state.
	failure, exchangeErr := host.serve(helperContext)
	if exchangeErr != nil {
		cancel()
	}
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return &debugLuaFailure{Failure: &debugError{Code: "interrupted", Context: "scriptStop", Message: ctx.Err().Error()}}
	}
	if errors.Is(helperContext.Err(), context.DeadlineExceeded) {
		return &debugLuaFailure{Failure: &debugError{Code: "deadline_exceeded", Context: "scriptDeadline", Message: helperContext.Err().Error()}}
	}
	if exchangeErr != nil || waitErr != nil {
		return debugLuaGoFailure("helperExit", errors.Join(exchangeErr, waitErr))
	}
	return failure
}

func (e *debugSupervisor) closeLuaPipe(pipe io.Closer) {
	err := pipe.Close()
	if err != nil && !errors.Is(err, os.ErrClosed) {
		e.logFailure("Lua helper pipe close", err)
	}
}

func (e *debugLuaHost) serve(ctx context.Context) (*debugLuaFailure, error) {
	operationContext, cancel := context.WithCancel(ctx)
	frames := make(chan debugLuaIncoming, 1)
	completions := make(chan debugLuaCompletion, debugLuaPendingLimit)
	readerDone := make(chan struct{})
	workerGroup := &sync.WaitGroup{}
	stopClose := context.AfterFunc(operationContext, e.closePipes)
	defer e.finishExchange(cancel, stopClose, workerGroup, readerDone)
	go e.readFrames(operationContext, frames, readerDone)
	pendingFrames := make(map[uint64]debugLuaFrame)
	for {
		select {
		case <-operationContext.Done():
			return nil, fmt.Errorf("helperExchange: %w", operationContext.Err())
		case incoming := <-frames:
			if incoming.err != nil {
				return nil, fmt.Errorf("helperFrame: %w", incoming.err)
			}
			frame := incoming.frame
			if frame.Version != 1 {
				return nil, errors.New("unsupported helper frame version")
			}
			if frame.Kind == "finished" {
				if frame.Sequence != e.sequence || len(pendingFrames) != 0 || frame.Request != nil || frame.Bootstrap != nil || len(frame.Result) != 0 {
					return nil, errors.New("helper finished with an invalid sequence or outstanding operations")
				}
				if frame.Failure == nil {
					return nil, nil
				}
				return &debugLuaFailure{Failure: frame.Failure, Traceback: frame.Traceback}, nil
			}
			if frame.Kind != "operation" || frame.Sequence == 0 || frame.Sequence != e.sequence+1 || frame.Request == nil || frame.Bootstrap != nil || len(frame.Result) != 0 || frame.Failure != nil || frame.Traceback != "" {
				return nil, errors.New("unexpected helper operation envelope")
			}
			if len(pendingFrames) >= debugLuaPendingLimit {
				return nil, errors.New("helper exceeds eight outstanding operations")
			}
			e.sequence = frame.Sequence
			pendingFrames[frame.Sequence] = frame
			workerGroup.Add(1)
			go e.executeOperation(operationContext, frame, completions, workerGroup)
		case completion := <-completions:
			pendingFrame, isPending := pendingFrames[completion.sequence]
			if !isPending || pendingFrame.Request.Operation != completion.operation {
				return nil, errors.New("helper completion does not match an outstanding operation")
			}
			err := e.sendCompletion(completion)
			if err != nil {
				return nil, fmt.Errorf("helperCompletion: %w", err)
			}
			delete(pendingFrames, completion.sequence)
		}
	}
}

func (e *debugLuaHost) readFrames(ctx context.Context, frames chan<- debugLuaIncoming, done chan<- struct{}) {
	defer close(done)
	for e.scanner.Scan() {
		var frame debugLuaFrame
		err := decodeDebugJSON(e.scanner.Bytes(), &frame)
		select {
		case frames <- debugLuaIncoming{frame: frame, err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
	err := e.scanner.Err()
	if err == nil {
		err = errors.New("helper exited without a final result")
	}
	select {
	case frames <- debugLuaIncoming{err: err}:
	case <-ctx.Done():
	}
}

func (e *debugLuaHost) executeOperation(ctx context.Context, frame debugLuaFrame, completions chan<- debugLuaCompletion, workerGroup *sync.WaitGroup) {
	defer workerGroup.Done()
	e.supervisor.logLuaActivity(fmt.Sprintf("[%d] %s", frame.Sequence, frame.Request.Operation))
	err := e.supervisor.automation.Enter(ctx)
	var result any
	if err == nil {
		result, err = e.supervisor.executeDebugRequest(ctx, *frame.Request)
		e.supervisor.automation.Leave()
	}
	completion := debugLuaCompletion{sequence: frame.Sequence, operation: frame.Request.Operation, result: result, err: err}
	select {
	case completions <- completion:
	case <-ctx.Done():
	}
}

func (e *debugLuaHost) sendCompletion(completion debugLuaCompletion) error {
	response := debugLuaFrame{Version: 1, Kind: "result", Sequence: completion.sequence}
	if completion.err != nil {
		response.Failure = debugErrorDTO(completion.err, 0)
	}
	if completion.result != nil {
		payload, err := json.Marshal(completion.result)
		if err != nil {
			return fmt.Errorf("helperResult: %w", err)
		}
		response.Result = payload
	}
	err := e.encoder.Encode(response)
	if err != nil {
		return fmt.Errorf("helperReply: %w", err)
	}
	return nil
}

func (e *debugLuaHost) finishExchange(cancel context.CancelFunc, stopClose func() bool, workerGroup *sync.WaitGroup, readerDone <-chan struct{}) {
	if !stopClose() {
		// Sync.Once also waits for an already-running cancellation close.
		e.closePipes()
	}
	cancel()
	e.closePipes()
	workerGroup.Wait()
	<-readerDone
}

func (e *debugLuaHost) closePipes() { e.closeOnce.Do(e.closeOwnedPipes) }

func (e *debugLuaHost) closeOwnedPipes() {
	for _, pipe := range []io.Closer{e.input, e.output} {
		if pipe == nil {
			continue
		}
		err := pipe.Close()
		if err != nil && !errors.Is(err, os.ErrClosed) {
			e.supervisor.logFailure("Lua helper exchange pipe close", err)
		}
	}
}
