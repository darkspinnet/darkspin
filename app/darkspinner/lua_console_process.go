//go:build scenario

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const launcherLuaWorkerArgument = "--scenario-supervisor-worker"

type launcherLuaProcess struct {
	console   *launcherLua
	command   *exec.Cmd
	input     io.WriteCloser
	done      chan struct{}
	stopOnce  sync.Once
	mutex     sync.Mutex
	discovery *debugDiscovery
	readers   sync.WaitGroup
}

func (e *launcherLua) launch(ctx context.Context, scriptPath string) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("luaContext: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("luaExecutable: %w", err)
	}
	directory := filepath.Dir(executable)
	command := exec.Command(executable, launcherLuaWorkerArgument, "--lua", scriptPath, "--lua-timeout", e.options.executionLimit.String())
	command.Dir = directory
	configureDebugLuaProcess(command)
	child := &launcherLuaProcess{console: e, command: command, done: make(chan struct{})}
	child.input, err = command.StdinPipe()
	if err != nil {
		return fmt.Errorf("luaInput: %w", err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		child.closePipe(child.input)
		return fmt.Errorf("luaOutput: %w", err)
	}
	errorOutput, err := command.StderrPipe()
	if err != nil {
		child.closePipe(output)
		child.closePipe(child.input)
		return fmt.Errorf("luaErrorOutput: %w", err)
	}
	err = command.Start()
	if err != nil {
		child.closePipe(errorOutput)
		child.closePipe(output)
		child.closePipe(child.input)
		return fmt.Errorf("luaProcess: %w", err)
	}
	e.mutex.Lock()
	e.child = child
	e.state.IsActive = true
	e.mutex.Unlock()
	child.readers.Add(2)
	go child.readOutput(output)
	go child.readOutput(errorOutput)
	go child.wait()
	return nil
}

func (e *launcherLuaProcess) readOutput(r io.Reader) {
	defer e.readers.Done()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Scenario discovery: ") {
			e.readDiscovery(strings.TrimPrefix(line, "Scenario discovery: "))
		} else {
			e.console.appendLine(line)
		}
		count, err := fmt.Fprintln(os.Stderr, line)
		if err != nil || count == 0 { /* The in-window console remains available without a terminal. */
		}
	}
	err := scanner.Err()
	if err != nil && !errors.Is(err, os.ErrClosed) {
		e.console.appendLine("Console stream: " + err.Error())
	}
}

func (e *launcherLuaProcess) readDiscovery(path string) {
	payload, err := os.ReadFile(path)
	if err != nil {
		e.console.appendLine("Scenario connection: " + err.Error())
		return
	}
	var discovery debugDiscovery
	err = json.Unmarshal(payload, &discovery)
	if err != nil {
		e.console.appendLine("Scenario discovery: " + err.Error())
		return
	}
	address, err := url.Parse(discovery.URL)
	if err != nil {
		e.console.appendLine("Invalid scenario endpoint")
		return
	}
	host := net.ParseIP(address.Hostname())
	if discovery.PID != e.command.Process.Pid || len(discovery.Token) < 32 || address.Scheme != "http" || host == nil || !host.IsLoopback() || address.Path != debugAPIPrefix {
		e.console.appendLine("Scenario connection did not match its owned process")
		return
	}
	e.mutex.Lock()
	e.discovery = &discovery
	e.mutex.Unlock()
	e.console.mutex.Lock()
	if e.console.child == e {
		e.console.state.RunID = discovery.ID
		e.console.state.Phase = "running"
	}
	e.console.mutex.Unlock()
}

func (e *launcherLuaProcess) wait() {
	e.readers.Wait()
	err := e.command.Wait()
	e.closePipe(e.input)
	e.console.mutex.Lock()
	if e.console.child == e {
		e.console.state.IsActive = false
		if e.console.state.Phase == "running" || e.console.state.Phase == "starting" {
			e.console.state.Phase = "stopped"
		}
	}
	e.console.mutex.Unlock()
	if err != nil {
		e.console.fail(fmt.Errorf("luaExit: %w", err))
	}
	close(e.done)
}

func (e *launcherLuaProcess) stop(ctx context.Context) error {
	e.stopOnce.Do(e.closeInput)
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	select {
	case <-e.done:
		return nil
	case <-timer.C:
		return errors.New("scenario cleanup is still running; wait before starting another script")
	}
}

func (e *launcherLuaProcess) closeInput() { e.closePipe(e.input) }
func (e *launcherLuaProcess) kill(ctx context.Context) error {
	select {
	case <-e.done:
		return nil
	default:
	}
	e.mutex.Lock()
	discovery := e.discovery
	e.mutex.Unlock()
	if discovery == nil {
		return errors.New("scenario connection is not ready")
	}
	requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestContext, http.MethodPost, discovery.URL+"/kill", nil)
	if err != nil {
		return fmt.Errorf("killRequest: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+discovery.Token)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: rejectScenarioReadyRedirect}
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("killSend: %w", err)
	}
	closeErr := response.Body.Close()
	if closeErr != nil {
		return fmt.Errorf("killResponse: %w", closeErr)
	}
	if response.StatusCode != http.StatusAccepted {
		return fmt.Errorf("scenario kill rejected (%d)", response.StatusCode)
	}
	err = e.stop(ctx)
	if err != nil {
		return fmt.Errorf("killWait: %w", err)
	}
	return nil
}
func (e *launcherLuaProcess) closePipe(pipe io.Closer) {
	err := pipe.Close()
	if err != nil && !errors.Is(err, os.ErrClosed) {
		e.console.appendLine("Console pipe close: " + err.Error())
	}
}

func (e *launcherLua) refresh(ctx context.Context) {
	if ctx == nil {
		return
	}
	e.mutex.Lock()
	child := e.child
	e.mutex.Unlock()
	if child == nil {
		return
	}
	select {
	case <-child.done:
		return
	default:
	}
	child.mutex.Lock()
	discovery := child.discovery
	child.mutex.Unlock()
	if discovery == nil {
		return
	}
	requestContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestContext, http.MethodGet, discovery.URL+"/status", nil)
	if err != nil {
		e.appendLine("Scenario status: " + err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+discovery.Token)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: rejectScenarioReadyRedirect}
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return
	}
	var status debugStatus
	decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(&status)
	closeErr := response.Body.Close()
	if decodeErr != nil || closeErr != nil || response.StatusCode != http.StatusOK {
		return
	}
	result := status.ScriptResult
	if result == nil {
		return
	}
	elapsed := result.FinishedAt.Sub(result.StartedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	e.mutex.Lock()
	if e.child == child {
		e.state.Phase = result.Outcome
		e.state.ElapsedMS = elapsed.Milliseconds()
		e.state.Failure = result.Error
	}
	e.mutex.Unlock()
}

func (e *launcherLuaProcess) runScript(ctx context.Context, path string) error {
	e.mutex.Lock()
	discovery := e.discovery
	e.mutex.Unlock()
	if discovery == nil {
		return errors.New("scenario connection is not ready")
	}
	payload, err := json.Marshal(struct {
		Path string `json:"path"`
	}{Path: path})
	if err != nil {
		return fmt.Errorf("runMarshal: %w", err)
	}
	requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestContext, http.MethodPost, discovery.URL+"/scripts/run", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("runRequest: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+discovery.Token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: rejectScenarioReadyRedirect}
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("runSend: %w", err)
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		return fmt.Errorf("runResponse: %w", errors.Join(readErr, closeErr))
	}
	if response.StatusCode != http.StatusAccepted {
		return fmt.Errorf("scenario run rejected (%d): %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	e.console.mutex.Lock()
	e.console.state.Script = path
	e.console.state.Phase = "running"
	e.console.state.Failure = ""
	e.console.state.ElapsedMS = 0
	e.console.mutex.Unlock()
	return nil
}
