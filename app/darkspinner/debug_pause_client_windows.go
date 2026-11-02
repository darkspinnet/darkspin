//go:build scenario && windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

type debugClientPause struct {
	mutex        sync.Mutex
	state        debugPauseState
	done         chan struct{}
	resumeOnce   sync.Once
	input        io.WriteCloser
	bridge       *controlPipe
	generation   string
	before       controlEnvelope
	client       *debugOwnedClient
	owner        debugWorldOwner
	host         *debugHost
	manager      *debugClientManager
	resumeTickMS uint64
}

func (e *debugClientPause) snapshot() debugPauseState {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.state
}
func (e *debugClientPause) resume() {
	e.resumeOnce.Do(e.closeInput)
}
func (e *debugClientPause) closeInput() {
	if e.input == nil {
		return
	} // A failed helper start never attached a debugger.
	err := e.input.Close()
	if err != nil {
		e.mutex.Lock()
		e.state.Failure = "resume pipe close: " + err.Error()
		e.mutex.Unlock()
	}
}

func (e *debugClientManager) DebugPauseState(clientID string) (debugPauseState, error) {
	e.mutex.Lock()
	client := e.clientsByID[clientID]
	e.mutex.Unlock()
	if client == nil {
		return debugPauseState{}, errors.New("debugger target is not owned")
	}
	client.mutex.Lock()
	pause := client.pause
	state := client.state
	client.mutex.Unlock()
	if pause == nil {
		return debugPauseState{Scope: "owned client debugger event only", ClientID: clientID, InstanceID: state.InstanceID, PID: state.ProcessID, Status: "idle"}, nil
	}
	return pause.snapshot(), nil
}

func (e *debugClientManager) ResumeDebugPause(ctx context.Context, clientID, token string) (debugPauseState, error) {
	e.mutex.Lock()
	client := e.clientsByID[clientID]
	e.mutex.Unlock()
	if client == nil {
		return debugPauseState{}, errors.New("debugger target is not owned")
	}
	client.mutex.Lock()
	pause := client.pause
	client.mutex.Unlock()
	if pause == nil || token == "" || pause.snapshot().PauseID != token {
		return debugPauseState{}, errors.New("debugger resume requires the exact owned pause_id")
	}
	pause.resume()
	select {
	case <-ctx.Done():
		return pause.snapshot(), fmt.Errorf("resumeWait: %w", ctx.Err())
	case <-pause.done:
		state := pause.snapshot()
		if state.Status != "resumed" {
			return state, errors.New("debugger recovery failed: " + state.Failure)
		}
		return state, nil
	}
}

func (e *debugOwnedClient) revokeDebugPause(ctx context.Context) error {
	e.mutex.Lock()
	pause := e.pause
	e.mutex.Unlock()
	if pause == nil {
		return nil
	}
	pause.resume()
	select {
	case <-ctx.Done():
		return fmt.Errorf("pauseRevoke: %w", ctx.Err())
	case <-pause.done:
		return nil
	}
}

func (e *debugClientManager) StartDebugPause(ctx context.Context, host *debugHost, owner debugWorldOwner, duration time.Duration, manifest string) (debugPauseState, error) {
	if duration <= 0 || duration > 5*time.Second {
		return debugPauseState{}, errors.New("debugger lease must be positive and at most five seconds")
	}
	e.mutex.Lock()
	client := e.clientsByID[owner.client.ClientID]
	isStopping := e.isStopping
	e.mutex.Unlock()
	if client == nil || isStopping {
		return debugPauseState{}, errors.New("owned debugger target is unavailable or stopping")
	}
	err := lockScenarioLiveness(ctx, &client.closeMutex)
	if err != nil {
		return debugPauseState{}, fmt.Errorf("pauseCloseLock: %w", err)
	}
	defer client.closeMutex.Unlock()
	err = lockScenarioLiveness(ctx, &client.captureMutex)
	if err != nil {
		return debugPauseState{}, fmt.Errorf("pauseCaptureLock: %w", err)
	}
	defer client.captureMutex.Unlock()
	client.mutex.Lock()
	previous := client.pause
	bridge := client.bridge
	processOwner := client.owner
	isClosing := client.isClosing
	client.mutex.Unlock()
	if isClosing {
		return debugPauseState{}, errors.New("owned client is closing")
	}
	if previous != nil {
		select {
		case <-previous.done:
		default:
			return previous.snapshot(), errors.New("owned client already has a debugger lease")
		}
	}
	if bridge == nil || processOwner == nil {
		return debugPauseState{}, errors.New("owned client has no debugger bridge or process handle")
	}
	err = recheckDebugWorldOwner(ctx, host, e, owner)
	if err != nil {
		return debugPauseState{}, fmt.Errorf("pauseIdentity: %w", err)
	}
	processOwner.mutex.Lock()
	var creation uint64
	if processOwner.process == nil {
		err = errors.New("owned process handle is unavailable")
	} else {
		creation, err = debugPauseCreation(processOwner.process.process)
	}
	processOwner.mutex.Unlock()
	if err != nil {
		return debugPauseState{}, fmt.Errorf("pauseProcess: %w", err)
	}
	token, err := scenarioRandomText()
	if err != nil {
		return debugPauseState{}, fmt.Errorf("pauseToken: %w", err)
	}
	started := time.Now()
	// Extend only this connection, for the bounded lease and recovery grace.
	before, generation, err := bridge.beginDebugPause(ctx, token, started.Add(duration+5*time.Second))
	if err != nil {
		return debugPauseState{}, fmt.Errorf("pauseFence: %w", err)
	}
	nowTick := uint64(windows.DurationSinceBoot() / time.Millisecond)
	if before.State == nil || !before.State.Frame.IsAvailable || nowTick < before.State.Frame.ObservedTickMS || nowTick-before.State.Frame.ObservedTickMS > 2000 {
		fenceErr := bridge.endDebugPause(token)
		return debugPauseState{}, fmt.Errorf("pauseBaseline: %w", errors.Join(errors.New("fresh native frame evidence is required before debugger attach"), fenceErr))
	}
	pause := &debugClientPause{state: debugPauseState{RunID: e.runID, ProcessCreation: fmt.Sprintf("%016x", creation), Scope: "owned client debugger event only", ClientID: owner.client.ClientID, InstanceID: owner.client.InstanceID, PID: owner.client.ProcessID, PauseID: token, Status: "preparing", StartedAt: started.UTC(), ExpiresAt: started.Add(duration).UTC(), PrePauseManifestPath: manifest}, done: make(chan struct{}), bridge: bridge, generation: generation, before: before, client: client, owner: owner, host: host, manager: e}
	command, input, output, job, err := startDebugPauseProcess(debugPauseBootstrap{PID: owner.client.ProcessID, Creation: creation, ParentPID: windows.GetCurrentProcessId(), DurationMS: uint32(duration / time.Millisecond), PauseID: token})
	if err != nil {
		fenceErr := bridge.endDebugPause(token)
		pause.mutex.Lock()
		pause.state.Status = "cleanup_failed"
		pause.state.Failure = errors.Join(err, fenceErr).Error()
		pause.mutex.Unlock()
		close(pause.done)
		client.mutex.Lock()
		client.pause = pause
		client.mutex.Unlock()
		return pause.snapshot(), fmt.Errorf("pauseHelper: %w", errors.Join(err, fenceErr))
	}
	pause.input = input
	client.mutex.Lock()
	client.pause = pause
	client.mutex.Unlock()
	acknowledgements := make(chan debugPauseWorkerReceipt, 1)
	go pause.monitor(client.lifetime, command, output, job, acknowledgements)
	select {
	case receipt := <-acknowledgements:
		if receipt.Status != "paused" {
			return pause.snapshot(), errors.New("debugger did not acknowledge a stopped client")
		}
		return pause.snapshot(), nil
	case <-pause.done:
		return pause.snapshot(), errors.New("debugger helper exited before stop acknowledgement: " + pause.snapshot().Failure)
	case <-ctx.Done():
		pause.resume()
		return pause.snapshot(), fmt.Errorf("pauseAcknowledge: %w", ctx.Err())
	}
}

func startDebugPauseProcess(req debugPauseBootstrap) (*exec.Cmd, io.WriteCloser, io.ReadCloser, windows.Handle, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, nil, nil, 0, fmt.Errorf("pauseExecutable: %w", err)
	}
	command := exec.Command(executable, "--scenario-debugger-worker")
	configureDebugLuaProcess(command)
	input, err := command.StdinPipe()
	if err != nil {
		return nil, nil, nil, 0, fmt.Errorf("pauseInput: %w", err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		closeErr := input.Close()
		return nil, nil, nil, 0, fmt.Errorf("pauseOutput: %w", errors.Join(err, closeErr))
	}
	command.Stderr = os.Stderr
	err = command.Start()
	if err != nil {
		return nil, nil, nil, 0, fmt.Errorf("pauseStart: %w", errors.Join(err, input.Close(), output.Close()))
	}
	// No bootstrap bytes are written until containment succeeds. The worker
	// cannot attach a debuggee while it is outside its parent-owned job.
	job, err := containDebugLuaProcess(command.Process)
	if err == nil {
		err = json.NewEncoder(input).Encode(req)
	}
	if err != nil {
		killErr := command.Process.Kill()
		waitErr := command.Wait()
		if job != 0 {
			err = errors.Join(err, closeDebugLuaJob(job))
		}
		return nil, nil, nil, 0, fmt.Errorf("pauseContain: %w", errors.Join(err, killErr, waitErr, input.Close(), output.Close()))
	}
	return command, input, output, job, nil
}

func (e *debugClientPause) monitor(lifetime context.Context, command *exec.Cmd, output io.ReadCloser, job windows.Handle, acks chan<- debugPauseWorkerReceipt) {
	defer close(e.done)
	watchDone := make(chan struct{})
	go e.watchLifetime(lifetime, command.Process, watchDone)
	decoder := json.NewDecoder(io.LimitReader(output, 8192))
	var stopped, resumed debugPauseWorkerReceipt
	err := decoder.Decode(&stopped)
	if err == nil && (stopped.Status != "paused" || stopped.ThreadID == 0 || stopped.At.IsZero()) {
		err = errors.New("invalid debugger stop receipt")
	}
	if err == nil {
		e.mutex.Lock()
		e.state.Status = "paused"
		e.state.IsPaused = true
		e.state.StoppedAt = stopped.At
		e.state.StopEventThreadID = stopped.ThreadID
		e.state.ExpiresAt = stopped.ExpiresAt
		e.mutex.Unlock()
		acks <- stopped
		err = decoder.Decode(&resumed)
		if err == nil && (resumed.Status != "resumed" || resumed.At.IsZero() || resumed.TickMS == 0) {
			err = errors.New("invalid debugger resume receipt")
		}
	}
	e.resume()
	waitErr := command.Wait()
	close(watchDone)
	closeErr := output.Close()
	if errors.Is(closeErr, os.ErrClosed) {
		closeErr = nil
	} // Wait already closes its stdout pipe.
	jobErr := closeDebugLuaJob(job)
	err = errors.Join(err, waitErr, closeErr, jobErr)
	e.mutex.Lock()
	e.state.IsPaused = false
	e.state.Status = "resuming"
	e.state.ResumedAt = resumed.At
	e.resumeTickMS = resumed.TickMS
	e.mutex.Unlock()
	if err == nil {
		recovery, cancel := context.WithTimeout(context.WithoutCancel(lifetime), 4*time.Second)
		err = e.recover(recovery)
		cancel()
	}
	if err != nil {
		// A detached but unhealthy client is stopped rather than advertised as
		// recovered. A helper crash already invokes Windows' kill fallback.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(lifetime), 2*time.Second)
		e.client.mutex.Lock()
		owner := e.client.owner
		e.client.mutex.Unlock()
		if owner != nil && stopped.Status == "paused" {
			err = errors.Join(err, owner.Close(cleanup))
		}
		cancel()
	}
	fenceErr := e.bridge.endDebugPause(e.snapshot().PauseID)
	err = errors.Join(err, fenceErr)
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if err != nil {
		e.state.Status = "cleanup_failed"
		e.state.Failure = err.Error()
		state := e.client.snapshot()
		if !state.IsRunning {
			e.state.Status = "terminated"
		}
		return
	}
	e.state.Status = "resumed"
	e.state.IsRecovered = true
}

func (e *debugClientPause) watchLifetime(ctx context.Context, process *os.Process, done <-chan struct{}) {
	// Independent of the worker's monotonic lease: terminate a hung debugger
	// helper rather than retaining an attached process indefinitely.
	watchdog := time.NewTimer(8 * time.Second)
	defer watchdog.Stop()
	select {
	case <-ctx.Done():
		e.resume()
	case <-watchdog.C:
		e.killHelper(process)
		return
	case <-done:
		return
	}
	grace := time.NewTimer(2 * time.Second)
	defer grace.Stop()
	select {
	case <-done:
	case <-grace.C:
		e.killHelper(process)
	}
}
func (e *debugClientPause) killHelper(process *os.Process) {
	err := process.Kill()
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		e.mutex.Lock()
		e.state.Failure = "debugger watchdog termination: " + err.Error()
		e.mutex.Unlock()
	}
}

func (e *debugClientPause) recover(ctx context.Context) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	state := e.snapshot()
	for {
		envelope, receivedAt, isConnected, failure := e.bridge.State()
		e.bridge.mutex.Lock()
		generation := e.bridge.generation
		e.bridge.mutex.Unlock()
		if !isConnected || generation != e.generation {
			return errors.New("debugger recovery lost the original bridge generation: " + failure)
		}
		if receivedAt.After(state.ResumedAt) && envelope.Sequence > e.before.Sequence {
			e.mutex.Lock()
			e.state.IsBridgeRecovered = true
			e.mutex.Unlock()
			err := recheckDebugWorldOwner(ctx, e.host, e.manager, e.owner)
			if err != nil {
				return fmt.Errorf("pauseRecoveredOwner: %w", err)
			}
			e.mutex.Lock()
			e.state.IsAccountRecovered = true
			e.mutex.Unlock()
			if envelope.State != nil && e.before.State != nil && envelope.State.Frame.IsAvailable && envelope.State.Frame.Sequence > e.before.State.Frame.Sequence && envelope.State.Frame.ObservedTickMS > e.before.State.Frame.ObservedTickMS && envelope.State.Frame.ObservedTickMS > e.resumeTickMS {
				e.mutex.Lock()
				e.state.IsFrameRecovered = true
				e.mutex.Unlock()
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("pauseFreshFrame: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
