//go:build scenario && windows

package main

import (
	"context"
	"debug/pe"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/darkspinnet/darkspin/server/scenario/control"
	"github.com/darkspinnet/darkspin/server/scenario/desktop"
	"golang.org/x/sys/windows"
)

var debugClientNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

type debugClientManager struct {
	mutex           sync.Mutex
	lifetime        context.Context
	runID           string
	cacheDirectory  string
	reportDirectory string
	clientsByID     map[string]*debugOwnedClient
	isStopping      bool
}

type debugOwnedClient struct {
	heroSelectionMutex    sync.Mutex
	abilityCastMutex      sync.Mutex
	isClosing             bool
	pause                 *debugClientPause
	mutex                 sync.Mutex
	closeMutex            sync.Mutex
	lifetime              context.Context
	cancel                context.CancelFunc
	startDone             chan struct{}
	owner                 *scenarioClient
	bridge                *controlPipe
	pipeName              string
	token                 string
	runID                 string
	state                 debugClientState
	failureCause          error
	helper                *debugDetachedProcess
	captureMutex          sync.Mutex
	screenshotSequence    uint32
	screenshotOutputBytes int64
}

func newDebugClientManager(ctx context.Context, runID, cacheDirectory, reportDirectory string) *debugClientManager {
	return &debugClientManager{lifetime: ctx, runID: runID, cacheDirectory: cacheDirectory, reportDirectory: reportDirectory, clientsByID: make(map[string]*debugOwnedClient)}
}

func (e *debugClientManager) Launch(ctx context.Context, req debugClientLaunchRequest) (debugClientState, error) {
	if !debugClientNamePattern.MatchString(req.ClientID) {
		return debugClientState{}, &control.ScenarioError{Code: control.InvalidRequest, Context: "clientName", Message: "client ID must be a lowercase name of at most 32 letters, digits, underscores or hyphens"}
	}
	host, port, err := net.SplitHostPort(req.ServerAddress)
	if err != nil {
		return debugClientState{}, fmt.Errorf("clientAddress: %w", err)
	}
	address := net.ParseIP(host)
	if address == nil || !address.IsLoopback() || port != "42127" {
		return debugClientState{}, errors.New("scenario clients require the owned loopback server on port 42127")
	}
	if req.JWT == "" || strings.ContainsRune(req.JWT, 0) {
		return debugClientState{}, errors.New("client launch requires an internal launch credential")
	}
	identity, err := scenarioRandomText()
	if err != nil {
		return debugClientState{}, fmt.Errorf("clientIdentity: %w", err)
	}
	token, err := scenarioRandomText()
	if err != nil {
		return debugClientState{}, fmt.Errorf("clientToken: %w", err)
	}
	lifetime, cancel := context.WithCancel(e.lifetime)
	client := &debugOwnedClient{lifetime: lifetime, cancel: cancel, startDone: make(chan struct{}), runID: e.runID, token: token, pipeName: `\\.\pipe\darkspin-` + identity,
		state: debugClientState{UserID: req.UserID, ClientID: req.ClientID, ProfileName: req.ProfileName, InstanceID: identity[:32], LaunchMode: "normal", IsModded: req.IsModded, Status: "launching"}}
	e.mutex.Lock()
	previous := e.clientsByID[req.ClientID]
	if e.isStopping || previous != nil {
		e.mutex.Unlock()
		cancel()
		return debugClientState{}, &control.ScenarioError{Code: control.Conflict, Context: "clientLaunch", Message: "client name is already reserved or the session is stopping"}
	}
	e.clientsByID[req.ClientID] = client
	e.mutex.Unlock()
	defer close(client.startDone)
	state, err := e.launchClient(ctx, client, req)
	if err != nil {
		client.mutex.Lock()
		client.failureCause = err
		if client.owner == nil {
			client.state.Status = "launch_failed"
			client.cancel()
		} else {
			client.state.Status = "inspection"
		}
		client.mutex.Unlock()
		return state, fmt.Errorf("clientLaunch: %w", err)
	}
	return state, nil
}

func (e *debugClientManager) launchClient(ctx context.Context, client *debugOwnedClient, req debugClientLaunchRequest) (debugClientState, error) {
	gamePath, workingDirectory, err := resolveGameExecutable(defaultGameExecutable(req.GameDirectory), req.GameDirectory)
	if err != nil {
		return debugClientState{}, fmt.Errorf("gameResolve: %w", err)
	}
	err = desktop.CheckRegular(gamePath)
	if err != nil {
		return debugClientState{}, fmt.Errorf("gameFile: %w", err)
	}
	err = validateGameData(workingDirectory)
	if err != nil {
		return debugClientState{}, fmt.Errorf("gameData: %w", err)
	}
	fangPath, err := existingAbsoluteFile(req.FangPath)
	if err != nil {
		return debugClientState{}, fmt.Errorf("fangPath: %w", err)
	}
	executableSHA, err := integrityFileSHA256(ctx, gamePath)
	if err != nil {
		return debugClientState{}, fmt.Errorf("gameHash: %w", err)
	}
	image, err := pe.Open(gamePath)
	if err != nil {
		return debugClientState{}, fmt.Errorf("gameImage: %w", err)
	}
	machine := image.Machine
	err = image.Close()
	if err != nil {
		return debugClientState{}, fmt.Errorf("imageClose: %w", err)
	}
	if machine != pe.IMAGE_FILE_MACHINE_I386 {
		return debugClientState{}, errors.New("scenario Fang requires the installed 32-bit game executable")
	}
	profileDirectory, reportDirectory, err := e.prepareClientDirectory(req.ClientID)
	if err != nil {
		return debugClientState{}, fmt.Errorf("clientDirectory: %w", err)
	}
	err = ensureWindowedClientPreference(profileDirectory)
	if err != nil {
		return debugClientState{}, fmt.Errorf("clientPreference: %w", err)
	}
	locales, err := detectClientLocales(req.GameDirectory)
	if err != nil {
		return debugClientState{}, fmt.Errorf("clientLocales: %w", err)
	}
	locale, err := resolveClientLocale("", locales)
	if err != nil {
		return debugClientState{}, fmt.Errorf("clientLocale: %w", err)
	}
	tracePath := filepath.Join(reportDirectory, "client.jsonl")
	environmentEntries := debugClientEnvironment(profileDirectory, reportDirectory, tracePath, req.JWT, client, req.IsIntroSkipped, req.IsCinematicSkipped, snapshotControlConnection{endpoint: req.SnapshotEndpoint, token: req.SnapshotToken, directory: req.SnapshotDirectory})
	for name, contents := range environmentEntries {
		if strings.ContainsRune(name, 0) || strings.ContainsRune(contents, 0) {
			return debugClientState{}, errors.New("invalid client environment")
		}
	}
	client.mutex.Lock()
	client.state.ProfileDirectory = profileDirectory
	client.state.TracePath = tracePath
	client.state.Observation.Identity = control.OwnedClientIdentity{Name: req.ClientID, InstanceID: client.state.InstanceID, ConnectionGeneration: 1, ExecutableSHA256: executableSHA, Architecture: "386"}
	client.mutex.Unlock()
	arguments := setClientLocaleArgument([]string{"-nolauncher", "-multipleInstances", "-userDataDir:" + profileDirectory}, locale)
	startup, cancelStartup := context.WithTimeout(ctx, 30*time.Second)
	defer cancelStartup()
	if req.IsDetached {
		return client.launchDetached(startup, req, gamePath, fangPath, profileDirectory, reportDirectory)
	}
	owner, capability, err := startScenarioClient(client.lifetime, startup, scenarioClientRequest{RunID: e.runID, GamePath: gamePath, WorkingDirectory: installDirectory(workingDirectory), FangPath: fangPath, Arguments: arguments, ServerAddress: req.ServerAddress, ModManifest: req.ModManifest, EnvironmentEntries: environmentEntries, Observer: client})
	delete(environmentEntries, launchJWTEnvironment)
	delete(environmentEntries, "DARKSPIN_DEBUG_TOKEN")
	if err != nil {
		return debugClientState{}, fmt.Errorf("clientStart: %w", err)
	}
	client.mutex.Lock()
	client.owner = owner
	client.state.Capability = capability
	client.state.Observation.Identity.BuildID = capability.BuildID
	client.state.Status = "suspended"
	client.mutex.Unlock()
	processID, err := owner.Resume(startup)
	if err != nil {
		return client.snapshot(), fmt.Errorf("clientResume: %w", err)
	}
	client.mutex.Lock()
	client.state.ProcessID = processID
	client.state.IsRunning = true
	client.state.Status = "running"
	client.mutex.Unlock()
	err = client.awaitBridge(startup)
	if err != nil {
		return client.snapshot(), fmt.Errorf("clientBridge: %w", err)
	}
	return client.snapshot(), nil
}

func (e *debugClientManager) prepareClientDirectory(clientID string) (string, string, error) {
	cacheParent := filepath.Join(e.cacheDirectory, "clients")
	reportParent := filepath.Join(e.reportDirectory, "clients")
	for _, parent := range []string{cacheParent, reportParent} {
		err := os.MkdirAll(parent, 0700)
		if err != nil {
			return "", "", fmt.Errorf("parentMkdir: %w", err)
		}
		err = desktop.CheckAncestors(parent)
		if err != nil {
			return "", "", fmt.Errorf("parentCheck: %w", err)
		}
	}
	identity, err := scenarioRandomText()
	if err != nil {
		return "", "", fmt.Errorf("directoryIdentity: %w", err)
	}
	directoryName := clientID + "-" + identity[:24]
	profileDirectory := filepath.Join(cacheParent, directoryName)
	err = os.Mkdir(profileDirectory, 0700)
	if err != nil {
		return "", "", fmt.Errorf("profileFresh: %w", err)
	}
	reportDirectory := filepath.Join(reportParent, directoryName)
	err = os.Mkdir(reportDirectory, 0700)
	if err != nil {
		return "", "", fmt.Errorf("reportFresh: %w", err)
	}
	for _, name := range []string{filepath.Join("AppData", "Roaming"), filepath.Join("AppData", "Local"), "Documents"} {
		err = os.MkdirAll(filepath.Join(profileDirectory, name), 0700)
		if err != nil {
			return "", "", fmt.Errorf("profileMkdir: %w", err)
		}
	}
	return profileDirectory, reportDirectory, nil
}

func debugClientEnvironment(profileDirectory, reportDirectory, tracePath, jwt string, client *debugOwnedClient, isIntroSkipped, isCinematicSkipped bool, connection snapshotControlConnection) map[string]string {
	volume := filepath.VolumeName(profileDirectory)
	introFlag := "0"
	if isIntroSkipped {
		introFlag = "1"
	}
	cinematicFlag := "0"
	if isCinematicSkipped {
		cinematicFlag = "1"
	}
	return map[string]string{
		"APPDATA":      filepath.Join(profileDirectory, "AppData", "Roaming"),
		"LOCALAPPDATA": filepath.Join(profileDirectory, "AppData", "Local"),
		"USERPROFILE":  profileDirectory, "HOMEDRIVE": volume, "HOMEPATH": strings.TrimPrefix(profileDirectory, volume),
		launchJWTEnvironment: jwt, "DARKSPIN_DEBUG_PIPE": client.pipeName, "DARKSPIN_DEBUG_TOKEN": client.token,
		"DARKSPIN_CLIENT_TRACE": tracePath, clientResultEnvironment: filepath.Join(reportDirectory, "client.failure.json"),
		clientLaunchEnvironment: client.state.InstanceID, clientProfileEnvironment: client.state.ClientID, clientModeEnvironment: "attached",
		"DARKSPIN_SKIP_INTRO": introFlag, "DARKSPIN_SKIP_CINEMATIC": cinematicFlag, "DARKSPIN_SNAPSHOT_MODE": "manual",
		snapshotEndpointEnvironment: connection.endpoint, snapshotTokenEnvironment: connection.token,
		snapshotDirectoryEnvironment: connection.directory,
	}
}

func (e *debugOwnedClient) BeforeResume(ctx context.Context, process uintptr, moduleHandle uint32, fangPath string) error {
	processID, err := windows.GetProcessId(windows.Handle(process))
	if err != nil {
		return fmt.Errorf("ownedPID: %w", err)
	}
	bridge, err := startControlPipe(e.lifetime, e.pipeName, e.token, processID, e.runID, e.state.InstanceID)
	if err != nil {
		return fmt.Errorf("bridgeStart: %w", err)
	}
	e.mutex.Lock()
	e.bridge = bridge
	e.token = ""
	e.state.ProcessID = processID
	e.state.Observation.Identity.PID = processID
	traceDirectory := filepath.Dir(e.state.TracePath)
	registration := clientProcessRecord{PID: processID, LaunchID: e.state.InstanceID, Profile: e.state.ClientID, Mode: "attached"}
	if e.state.IsDetached {
		registration.Mode = "detached"
	}
	e.mutex.Unlock()
	payload, err := json.Marshal(registration)
	if err != nil {
		return fmt.Errorf("ownedMarshal: %w", err)
	}
	err = os.WriteFile(filepath.Join(traceDirectory, registration.LaunchID+".process.json"), append(payload, '\n'), 0600)
	if err != nil {
		return fmt.Errorf("ownedWrite: %w", err)
	}
	return nil
}

func (e *debugOwnedClient) AfterResume(ctx context.Context, processID uint32) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("resumeContext: %w", err)
	}
	e.mutex.Lock()
	e.state.ProcessID = processID
	e.mutex.Unlock()
	return nil
}

func (e *debugOwnedClient) awaitBridge(ctx context.Context) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		e.mutex.Lock()
		bridge := e.bridge
		e.mutex.Unlock()
		if bridge != nil {
			envelope, receivedAt, isConnected, failure := bridge.State()
			if isConnected && envelope.State != nil && !receivedAt.IsZero() {
				return nil
			}
			if failure != "" {
				return errors.New("owned client bridge disconnected before its first observation")
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("bridgeContext: %w", ctx.Err())
		case <-e.lifetime.Done():
			return fmt.Errorf("bridgeLifetime: %w", e.lifetime.Err())
		case <-ticker.C:
		}
	}
}

func (e *debugClientManager) State(clientID string) (debugClientState, error) {
	e.mutex.Lock()
	client := e.clientsByID[clientID]
	e.mutex.Unlock()
	if client == nil {
		return debugClientState{}, &control.ScenarioError{Code: control.NotFound, Context: "ownedClient", Message: "client is not owned by this session"}
	}
	return client.snapshot(), nil
}

func (e *debugClientManager) List() []debugClientState {
	e.mutex.Lock()
	clients := make([]*debugOwnedClient, 0, len(e.clientsByID))
	for _, client := range e.clientsByID {
		clients = append(clients, client)
	}
	e.mutex.Unlock()
	states := make([]debugClientState, 0, len(clients))
	for _, client := range clients {
		states = append(states, client.snapshot())
	}
	sort.Slice(states, func(left, right int) bool { return states[left].ClientID < states[right].ClientID })
	return states
}

func (e *debugOwnedClient) snapshot() debugClientState {
	e.mutex.Lock()
	state := e.state
	state.Capability.Features = append([]string(nil), e.state.Capability.Features...)
	bridge := e.bridge
	owner := e.owner
	e.mutex.Unlock()
	unavailable := control.Availability{Status: control.Unavailable, Reason: "not_observed"}
	state.Observation.Bridge = unavailable
	state.Observation.FrameSequence.Availability = unavailable
	state.Observation.SceneAssetID.Availability = unavailable
	state.Observation.SceneName.Availability = unavailable
	state.Observation.ControlledHeroID.Availability = unavailable
	state.Observation.SelectedHeroSlot.Availability = unavailable
	if owner != nil {
		owner.mutex.Lock()
		if owner.process != nil {
			status, err := windows.WaitForSingleObject(owner.process.process, 0)
			if err == nil {
				state.IsRunning = status != windows.WAIT_OBJECT_0
				if !state.IsRunning && state.Status != "closed" {
					state.Status = "exited"
				}
			} else {
				state.Status = "liveness_unavailable"
				e.mutex.Lock()
				e.failureCause = fmt.Errorf("livenessQuery: %w", err)
				e.mutex.Unlock()
			}
		} else {
			state.IsRunning = false
		}
		owner.mutex.Unlock()
	}
	if bridge == nil {
		return state
	}
	envelope, receivedAt, isConnected, failure := bridge.State()
	if envelope.State == nil {
		if failure != "" {
			state.Observation.Bridge = control.Availability{Status: control.Disconnected, Reason: failure}
		}
		return state
	}
	observation := &state.Observation
	observation.Sequence = envelope.Sequence
	observation.ObservedAt = receivedAt
	availability := control.Availability{Status: control.Available, ObservedAt: receivedAt, Sequence: envelope.Sequence}
	if !isConnected {
		availability.Status = control.Disconnected
		availability.Reason = "bridge_disconnected"
	} else if time.Since(receivedAt) > 2*time.Second {
		availability.Status = control.Stale
		availability.Reason = "bridge_heartbeat_expired"
	}
	if bridge.debugPauseActive() {
		availability.Status = control.Stale
		availability.Reason = "owned_debugger_pause_cached"
		state.Status = "debugger_pause"
	}
	observation.Bridge = availability
	native := envelope.State
	nowTick := uint64(windows.DurationSinceBoot() / time.Millisecond)
	if native.Frame.IsAvailable {
		frameAvailability := availability
		if nowTick < native.Frame.ObservedTickMS || nowTick-native.Frame.ObservedTickMS > 2000 {
			frameAvailability.Status = control.Stale
			frameAvailability.Reason = "native_frame_not_progressing"
		}
		observation.FrameSequence = control.Observation[uint64]{Availability: frameAvailability, Data: uint64(native.Frame.Sequence)}
	}
	if native.Scene.IsAvailable {
		observation.SceneAssetID = control.Observation[uint32]{Availability: availability, Data: native.Scene.AssetID}
	}
	return state
}

func (e *debugClientManager) Close(ctx context.Context, clientID string, isTerminate bool) error {
	e.mutex.Lock()
	client := e.clientsByID[clientID]
	e.mutex.Unlock()
	if client == nil {
		return &control.ScenarioError{Code: control.NotFound, Context: "ownedClose", Message: "client is not owned by this session"}
	}
	client.closeMutex.Lock()
	client.mutex.Lock()
	client.isClosing = true
	client.mutex.Unlock()
	// Wait for attach preparation, then release the lifecycle lock before
	// waiting for detach/recovery. New pauses see isClosing and are rejected.
	client.closeMutex.Unlock()
	pauseErr := client.revokeDebugPause(ctx)
	if pauseErr != nil && !isTerminate {
		return fmt.Errorf("closePause: %w", pauseErr)
	}
	client.closeMutex.Lock()
	defer client.closeMutex.Unlock()
	select {
	case <-client.startDone:
	case <-ctx.Done():
		return fmt.Errorf("launchWait: %w", ctx.Err())
	}
	client.mutex.Lock()
	owner := client.owner
	bridge := client.bridge
	helper := client.helper
	client.mutex.Unlock()
	if owner != nil {
		var err error
		if isTerminate {
			err = owner.Close(ctx)
		} else {
			err = closeDebugClientWindow(ctx, owner)
		}
		if err != nil {
			return fmt.Errorf("ownedClose: %w", err)
		}
	}
	if helper != nil {
		err := helper.Close(ctx, isTerminate)
		if err != nil {
			return fmt.Errorf("helperClose: %w", err)
		}
	}
	client.cancel()
	if bridge != nil {
		err := bridge.Close()
		if err != nil {
			return fmt.Errorf("bridgeClose: %w", err)
		}
	}
	client.mutex.Lock()
	client.state.Status = "closed"
	client.state.IsRunning = false
	client.mutex.Unlock()
	return nil
}

func (e *debugClientManager) Forget(clientID string) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	client := e.clientsByID[clientID]
	if client == nil {
		return nil
	}
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.state.Status != "closed" {
		return errors.New("close the owned client before releasing its launch name")
	}
	delete(e.clientsByID, clientID)
	return nil
}

func (e *debugClientManager) Shutdown(ctx context.Context) error {
	e.mutex.Lock()
	e.isStopping = true
	clients := make([]*debugOwnedClient, 0, len(e.clientsByID))
	for _, client := range e.clientsByID {
		clients = append(clients, client)
	}
	e.mutex.Unlock()
	var shutdownErr error
	for _, client := range clients {
		graceful, cancelGraceful := context.WithTimeout(ctx, 3*time.Second)
		err := e.Close(graceful, client.state.ClientID, false)
		cancelGraceful()
		if err == nil {
			continue
		}
		client.mutex.Lock()
		client.failureCause = fmt.Errorf("gracefulShutdown: %w", err)
		client.mutex.Unlock()
		force, cancelForce := context.WithTimeout(ctx, 3*time.Second)
		forceErr := e.Close(force, client.state.ClientID, true)
		cancelForce()
		if forceErr != nil {
			shutdownErr = errors.Join(shutdownErr, fmt.Errorf("clientShutdown: %w", errors.Join(err, forceErr)))
		}
	}
	if shutdownErr != nil {
		return fmt.Errorf("ownedShutdown: %w", shutdownErr)
	}
	return nil
}

func (e *debugClientManager) Terminate(ctx context.Context) error {
	e.mutex.Lock()
	e.isStopping = true
	e.mutex.Unlock()
	var terminateErr error
	for _, client := range e.List() {
		err := e.Close(ctx, client.ClientID, true)
		if err != nil {
			terminateErr = errors.Join(terminateErr, fmt.Errorf("clientTerminate[%s]: %w", client.ClientID, err))
		}
	}
	if terminateErr != nil {
		return fmt.Errorf("ownedTerminate: %w", terminateErr)
	}
	return nil
}

func closeDebugClientWindow(ctx context.Context, owner *scenarioClient) error {
	owner.mutex.Lock()
	process := owner.process
	owner.mutex.Unlock()
	if process == nil {
		return nil
	}
	status, err := windows.WaitForSingleObject(process.process, 0)
	if err != nil {
		return fmt.Errorf("closeStatus: %w", err)
	}
	if status != windows.WAIT_OBJECT_0 {
		scan := scenarioEntryWindowScan{processID: process.processID}
		result, enumErr := callScenarioEntry(entryEnumWindows, entryWindowCallback, uintptr(unsafe.Pointer(&scan)))
		if scan.err != nil {
			return fmt.Errorf("closeScan: %w", scan.err)
		}
		if enumErr != nil {
			return fmt.Errorf("closeEnumerate: %w", enumErr)
		}
		if result == 0 || len(scan.windows) == 0 {
			return errors.New("owned client has no closeable visible window; explicit termination remains available")
		}
		postClose := entryUser32.NewProc("PostMessageW")
		for _, window := range scan.windows {
			var processID uint32
			threadID, identityErr := callScenarioEntry(entryWindowPID, window, uintptr(unsafe.Pointer(&processID)))
			if identityErr != nil {
				return fmt.Errorf("closeIdentity: %w", identityErr)
			}
			if threadID == 0 || processID != process.processID {
				return errors.New("owned window identity changed before close")
			}
			posted, postErr := callScenarioEntry(postClose, window, 0x0010, 0, 0)
			if postErr != nil {
				return fmt.Errorf("closePost: %w", postErr)
			}
			if posted == 0 {
				return errors.New("owned window close was not queued")
			}
		}
		for {
			contextErr := ctx.Err()
			if contextErr != nil {
				return fmt.Errorf("closeContext: %w", contextErr)
			}
			waitStatus, waitErr := windows.WaitForSingleObject(process.process, 50)
			if waitErr != nil {
				return fmt.Errorf("closeWait: %w", waitErr)
			}
			if waitStatus == windows.WAIT_OBJECT_0 {
				break
			}
			if waitStatus != uint32(windows.WAIT_TIMEOUT) {
				return errors.New("unexpected owned client close wait status")
			}
		}
	}
	owner.mutex.Lock()
	owner.cancellation.stop()
	process.closeHandles()
	owner.process = nil
	owner.mutex.Unlock()
	return nil
}
