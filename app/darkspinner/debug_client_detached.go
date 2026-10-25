//go:build scenario && windows

package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/darkspinnet/darkspin/server/scenario"
	"golang.org/x/sys/windows"
)

type debugDetachedMessage struct {
	Kind          string                      `json:"kind"`
	Token         string                      `json:"token,omitempty"`
	JWT           string                      `json:"jwt,omitempty"`
	Arguments     []string                    `json:"arguments,omitempty"`
	GameDirectory string                      `json:"game_directory,omitempty"`
	ProcessID     uint32                      `json:"process_id,omitempty"`
	Capability    *scenarioCapabilityResponse `json:"capability,omitempty"`
}

type debugDetachedProcess struct {
	mutex        sync.Mutex
	command      *exec.Cmd
	stdin        io.WriteCloser
	stdout       io.ReadCloser
	logOutput    *os.File
	done         chan struct{}
	failureCause error
}

// launchDetached uses the existing --detached-game flow in a supervised helper.
// The private bootstrap extends its ownership handoff without exposing a JWT in
// process arguments or changing the installed game.
func (e *debugOwnedClient) launchDetached(ctx context.Context, req debugClientLaunchRequest, gamePath, fangPath, profileDirectory, reportDirectory string) (debugClientState, error) {
	executable, err := os.Executable()
	if err != nil {
		return debugClientState{}, fmt.Errorf("helperExecutable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return debugClientState{}, fmt.Errorf("helperResolve: %w", err)
	}
	logOutput, err := os.OpenFile(filepath.Join(reportDirectory, "detached-helper.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return debugClientState{}, fmt.Errorf("helperLog: %w", err)
	}
	command := exec.Command(executable, "--scenario-detached-worker")
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP, HideWindow: true}
	command.Stderr = logOutput
	// Each helper starts from its own immutable block. The JWT travels only in
	// the private inherited bootstrap pipe, then its native child's environment.
	entries := debugClientEnvironment(profileDirectory, reportDirectory, e.state.TracePath, "", e, req.IsIntroSkipped, req.IsCinematicSkipped, snapshotControlConnection{endpoint: req.SnapshotEndpoint, token: req.SnapshotToken, directory: req.SnapshotDirectory})
	command.Env = debugDetachedEnvironment(entries)
	stdin, err := command.StdinPipe()
	if err != nil {
		closeDebugHelperLog(logOutput)
		return debugClientState{}, fmt.Errorf("helperInput: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		closeDebugHelperPipe(stdin)
		closeDebugHelperLog(logOutput)
		return debugClientState{}, fmt.Errorf("helperOutput: %w", err)
	}
	err = command.Start()
	if err != nil {
		closeDebugHelperPipe(stdin)
		closeDebugHelperPipe(stdout)
		closeDebugHelperLog(logOutput)
		return debugClientState{}, fmt.Errorf("helperStart: %w", err)
	}
	helper := &debugDetachedProcess{command: command, stdin: stdin, stdout: stdout, logOutput: logOutput, done: make(chan struct{})}
	e.mutex.Lock()
	e.helper = helper
	e.mutex.Unlock()
	go helper.wait()
	stopStartup := context.AfterFunc(ctx, helper.abortStartup)
	defer stopStartup()
	stopLifetime := context.AfterFunc(e.lifetime, helper.disconnectParent)
	// The lifetime callback remains registered until helper exit, so supervisor
	// cancellation is independent of a later script failure.
	go helper.settleLifetime(stopLifetime)
	token := e.token
	arguments := []string{"--detached-game", "--multiple-instances", "--game-exe", gamePath, "--fang", fangPath, "--user-data-dir", profileDirectory,
		"--client-profile", req.ClientID, "--server-address", req.ServerAddress, "--launch-id", e.state.InstanceID,
		"--client-result", filepath.Join(reportDirectory, "client.failure.json"), "--client-trace", e.state.TracePath}
	if req.IsCinematicSkipped {
		arguments = append(arguments, "--skip-cinematic")
	}
	if req.IsModded {
		arguments = append(arguments, "--mod-manifest", req.ModManifest)
	}
	if req.IsIntroSkipped {
		arguments = append(arguments, "--skip-intro")
	}
	err = writeDebugDetachedMessage(stdin, debugDetachedMessage{Kind: "bootstrap", Token: token, JWT: req.JWT, Arguments: arguments, GameDirectory: req.GameDirectory})
	if err != nil {
		return e.snapshot(), fmt.Errorf("helperBootstrap: %w", err)
	}
	ready, err := readDebugDetachedMessage(stdout)
	if err != nil {
		return e.snapshot(), fmt.Errorf("helperReady: %w", err)
	}
	if ready.Kind != "ready" || ready.ProcessID == 0 || ready.Capability == nil || subtle.ConstantTimeCompare([]byte(ready.Token), []byte(token)) != 1 {
		return e.snapshot(), errors.New("invalid owned detached helper handshake")
	}
	compiled := scenarioCapability()
	if ready.Capability.Component != "fang" || ready.Capability.BuildID != compiled.BuildID || ready.Capability.ProtocolVersion != scenario.ProtocolVersion {
		return e.snapshot(), errors.New("detached helper loaded a mismatched Fang capability")
	}
	processHandle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, ready.ProcessID)
	if err != nil {
		return e.snapshot(), fmt.Errorf("ownedHandle: %w", err)
	}
	process := &injectedProcess{process: processHandle, processID: ready.ProcessID, isResumed: true}
	owner := &scenarioClient{runID: e.runID, process: process, processID: ready.ProcessID}
	owner.cancellation = process.watch(e.lifetime)
	e.mutex.Lock()
	e.owner = owner
	e.state.IsDetached = true
	e.state.LaunchMode = "detached"
	e.state.ProcessID = ready.ProcessID
	e.state.Capability = scenario.Capability{ProtocolVersion: ready.Capability.ProtocolVersion, Component: ready.Capability.Component, BuildID: ready.Capability.BuildID, Features: append([]string(nil), ready.Capability.Features...)}
	e.state.Observation.Identity.BuildID = ready.Capability.BuildID
	e.state.Observation.Identity.PID = ready.ProcessID
	e.mutex.Unlock()
	err = e.BeforeResume(ctx, uintptr(processHandle), 0, fangPath)
	if err != nil {
		return e.snapshot(), fmt.Errorf("detachedBridge: %w", err)
	}
	err = writeDebugDetachedMessage(stdin, debugDetachedMessage{Kind: "accepted", Token: token})
	if err != nil {
		return e.snapshot(), fmt.Errorf("helperAccept: %w", err)
	}
	resumed, err := readDebugDetachedMessage(stdout)
	if err != nil {
		return e.snapshot(), fmt.Errorf("helperResume: %w", err)
	}
	if resumed.Kind != "resumed" || resumed.ProcessID != ready.ProcessID {
		return e.snapshot(), errors.New("detached native process did not confirm resume")
	}
	e.mutex.Lock()
	e.state.Status = "running"
	e.state.IsRunning = true
	e.mutex.Unlock()
	err = e.awaitBridge(ctx)
	if err != nil {
		return e.snapshot(), fmt.Errorf("detachedObservation: %w", err)
	}
	return e.snapshot(), nil
}

func debugDetachedEnvironment(overrides map[string]string) []string {
	entries := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		isOverridden := false
		for name := range overrides {
			if len(entry) > len(name) && entry[len(name)] == '=' && strings.EqualFold(entry[:len(name)], name) {
				isOverridden = true
				break
			}
		}
		if !isOverridden {
			entries = append(entries, entry)
		}
	}
	for name, contents := range overrides {
		entries = append(entries, name+"="+contents)
	}
	return entries
}

func (e *debugDetachedProcess) wait() {
	err := e.command.Wait()
	e.mutex.Lock()
	if err != nil {
		e.failureCause = fmt.Errorf("helperWait: %w", err)
	}
	e.mutex.Unlock()
	closeDebugHelperLog(e.logOutput)
	close(e.done)
}

func (e *debugDetachedProcess) settleLifetime(stop func() bool) {
	<-e.done
	isStopped := stop()
	if !isStopped { /* Concurrent parent disconnection only closes the helper input. */
	}
}

func (e *debugDetachedProcess) disconnectParent() { closeDebugHelperPipe(e.stdin) }

func (e *debugDetachedProcess) abortStartup() {
	e.disconnectParent()
	err := e.command.Process.Kill()
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		e.mutex.Lock()
		e.failureCause = fmt.Errorf("helperAbort: %w", err)
		e.mutex.Unlock()
	}
}

func (e *debugDetachedProcess) Close(ctx context.Context, isTerminate bool) error {
	defer e.disconnectParent()
	if isTerminate {
		e.abortStartup()
	}
	select {
	case <-e.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("helperClose: %w", ctx.Err())
	}
}

func closeDebugHelperPipe(w io.Closer) {
	err := w.Close()
	if err != nil { /* The peer exit/retained handle establishes final cleanup. */
	}
}
func closeDebugHelperLog(w *os.File) {
	err := w.Close()
	if err != nil { /* Helper completion remains observable through its owned process. */
	}
}

func writeDebugDetachedMessage(w io.Writer, message debugDetachedMessage) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("helperMarshal: %w", err)
	}
	if len(payload) == 0 || len(payload) > controlLimit {
		return errors.New("helper message exceeds limit")
	}
	packet := make([]byte, 4+len(payload))
	binary.LittleEndian.PutUint32(packet, uint32(len(payload)))
	copy(packet[4:], payload)
	count, err := w.Write(packet)
	if err != nil {
		return fmt.Errorf("helperWrite: %w", err)
	}
	if count != len(packet) {
		return fmt.Errorf("helperShort: %w", io.ErrShortWrite)
	}
	return nil
}

func readDebugDetachedMessage(r io.Reader) (debugDetachedMessage, error) {
	var message debugDetachedMessage
	var header [4]byte
	headerCount, err := io.ReadFull(r, header[:])
	if err != nil {
		return message, fmt.Errorf("helperHeader: %w", err)
	}
	if headerCount != len(header) {
		return message, errors.New("incomplete helper header")
	}
	length := binary.LittleEndian.Uint32(header[:])
	if length == 0 || length > controlLimit {
		return message, errors.New("invalid helper message size")
	}
	payload := make([]byte, int(length))
	payloadCount, err := io.ReadFull(r, payload)
	if err != nil {
		return message, fmt.Errorf("helperPayload: %w", err)
	}
	if payloadCount != len(payload) {
		return message, errors.New("incomplete helper payload")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&message)
	if err != nil {
		return message, fmt.Errorf("helperDecode: %w", err)
	}
	var trailing any
	err = decoder.Decode(&trailing)
	if err != nil && !errors.Is(err, io.EOF) {
		return message, fmt.Errorf("helperTrailing: %w", err)
	}
	if err == nil {
		return message, errors.New("helper message contains trailing JSON")
	}
	return message, nil
}
