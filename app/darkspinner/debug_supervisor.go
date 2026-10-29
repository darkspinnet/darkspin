//go:build scenario

package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario/control"
	"github.com/darkspinnet/darkspin/server/scenario/desktop"
)

const debugAPIPrefix = "/dev/scenario/v1"

type debugSupervisor struct {
	registry        *control.Registry
	token           string
	authority       string
	id              string
	scriptRoot      string
	gameDirectory   string
	reportDirectory string
	operationMutex  sync.Mutex
	operationState  *debugOperationState
	diagnosticCount uint32
	executionLimit  time.Duration
	automation      debugAutomation
	sessionContext  context.Context
	stop            chan struct{}
	stopOnce        sync.Once
	isLauncherOwned bool
	isKillRequested atomic.Bool
	cancelSession   context.CancelFunc
	scriptMutex     sync.Mutex
	scriptControl   debugScriptControl
	scriptRequests  chan string
}

type debugDiscovery struct {
	Schema    int       `json:"schema"`
	ID        string    `json:"id"`
	PID       int       `json:"pid"`
	URL       string    `json:"url"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

func runDebugSupervisor(arguments []string) (bool, error) {
	isPauseWorker, pauseErr := runDebugPauseWorker(arguments)
	if isPauseWorker {
		if pauseErr != nil {
			return true, fmt.Errorf("debuggerWorker: %w", pauseErr)
		}
		return true, nil
	}
	isDetachedWorker, detachedErr := runDebugDetachedWorker(arguments)
	if isDetachedWorker {
		if detachedErr != nil {
			return true, fmt.Errorf("debugDetached: %w", detachedErr)
		}
		return true, nil
	}
	isWorker, workerErr := runDebugLuaWorker(arguments)
	if isWorker {
		if workerErr != nil {
			return true, fmt.Errorf("luaWorker: %w", workerErr)
		}
		return true, nil
	}
	isRequested := false
	isLauncherOwned := false
	isAPI := false
	filteredArguments := []string{}
	for _, argument := range arguments {
		if argument == "--lua-headless" || strings.HasPrefix(argument, "--lua-headless=") {
			argument = strings.Replace(argument, "--lua-headless", "--lua", 1)
			isAPI = true
			isRequested = true
		}
		if argument == launcherLuaWorkerArgument {
			isLauncherOwned = true
			continue
		}
		filteredArguments = append(filteredArguments, argument)
		name := argument
		separator := strings.IndexByte(argument, '=')
		if separator >= 0 {
			name = argument[:separator]
		}
		if name == "--debug-api" || name == "--lua" || name == "--lua-timeout" || name == "--scenario-script-worker" || name == "--scenario-detached-worker" || name == "--scenario-debugger-worker" {
			isRequested = true
		}
		if name == "--debug-api" {
			isAPI = true
		}
	}
	if !isRequested {
		return false, nil
	}
	if !isLauncherOwned && !isAPI {
		return false, nil
	}
	arguments = filteredArguments
	if runtime.GOOS != "windows" {
		return true, errors.New("scenario debug supervisor requires Windows")
	}
	options, err := parseDebugLaunchOptions(arguments)
	if err != nil {
		return true, fmt.Errorf("debugArguments: %w", err)
	}
	err = ensureStandardUser()
	if err != nil {
		return true, fmt.Errorf("debugPrivilege: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return true, fmt.Errorf("debugExecutable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return true, fmt.Errorf("debugResolve: %w", err)
	}
	gameDirectory := filepath.Dir(executable)
	root := debugWorkspaceRoot(gameDirectory)
	scriptRoot, err := resolveDebugLuaRoot(gameDirectory)
	if err != nil {
		return true, fmt.Errorf("debugLuaRoot: %w", err)
	}
	entropy := make([]byte, 32)
	count, err := rand.Read(entropy)
	if err != nil || count != len(entropy) {
		return true, fmt.Errorf("debugEntropy: %w", errors.Join(err, errors.New("could not allocate supervisor identity")))
	}
	id := "supervisor-" + hex.EncodeToString(entropy[:12])
	registry, err := control.NewRegistry(id)
	if err != nil {
		return true, fmt.Errorf("debugRegistry: %w", err)
	}
	supervisor := &debugSupervisor{registry: registry, id: id, scriptRequests: make(chan string, 1),
		isLauncherOwned: isLauncherOwned,
		executionLimit:  options.executionLimit,
		token:           hex.EncodeToString(entropy), stop: make(chan struct{}),
		scriptRoot:      scriptRoot,
		gameDirectory:   gameDirectory,
		reportDirectory: filepath.Join(root, "bin", "game", "logs", "scenarios", id)}
	err = supervisor.serve(root, options.scriptName)
	if err != nil {
		return true, fmt.Errorf("debugServe: %w", err)
	}
	return true, nil
}

func parseDebugArguments(arguments []string) (string, error) {
	scriptName := ""
	isAPI := false
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case argument == "--debug-api" && !isAPI:
			isAPI = true
		case argument == "--lua" && scriptName == "":
			index++
			if index >= len(arguments) || arguments[index] == "" {
				return "", errors.New("--lua requires a relative source file")
			}
			scriptName = arguments[index]
		case strings.HasPrefix(argument, "--lua=") && scriptName == "":
			scriptName = strings.TrimPrefix(argument, "--lua=")
			if scriptName == "" {
				return "", errors.New("--lua requires a relative source file")
			}
		default:
			return "", fmt.Errorf("unsupported scenario supervisor argument %q", argument)
		}
	}
	return scriptName, nil
}

func debugWorkspaceRoot(directory string) string {
	for current := directory; ; current = filepath.Dir(current) {
		if strings.EqualFold(filepath.Base(current), "bin") {
			return filepath.Dir(current)
		}
		if filepath.Dir(current) == current {
			return directory
		}
	}
}

func (e *debugSupervisor) serve(root, scriptName string) error {
	parent := filepath.Join(root, "bin", "cache", "scenario")
	err := os.MkdirAll(parent, 0700)
	if err != nil {
		return fmt.Errorf("debugParent: %w", err)
	}
	err = desktop.CheckAncestors(parent)
	if err != nil {
		return fmt.Errorf("debugAncestors: %w", err)
	}
	privateDirectory := filepath.Join(parent, e.id)
	err = createDebugPrivateDirectory(privateDirectory)
	if err != nil {
		return fmt.Errorf("debugPrivate: %w", err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("debugListen: %w", err)
	}
	e.authority = listener.Addr().String()
	server := &http.Server{Handler: e, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: debugLuaExecutionLimit + 5*time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8192}
	discoveryPath := filepath.Join(privateDirectory, "discovery.json")
	err = writeDebugJSON(discoveryPath, debugDiscovery{Schema: 1, ID: e.id, PID: os.Getpid(),
		URL: "http://" + listener.Addr().String() + debugAPIPrefix, Token: e.token, CreatedAt: time.Now().UTC()})
	if err != nil {
		closeErr := listener.Close()
		return fmt.Errorf("debugDiscovery: %w", errors.Join(err, closeErr))
	}
	defer e.removeDiscovery(discoveryPath)
	sessionContext, sessionCancel := context.WithCancel(context.Background())
	defer sessionCancel()
	e.sessionContext = sessionContext
	e.cancelSession = sessionCancel
	if scriptName != "" {
		err = e.registry.BeginScript()
		if err != nil {
			closeErr := listener.Close()
			return fmt.Errorf("debugScriptLease: %w", errors.Join(err, closeErr))
		}
	}
	results := make(chan error, 1)
	go serveDebugHTTP(server, listener, results)
	if e.isLauncherOwned {
		go e.watchLauncherInput()
	}
	count, outputErr := fmt.Fprintln(os.Stderr, "Scenario discovery:", discoveryPath)
	if outputErr != nil || count == 0 {
		// Discovery remains available on disk when stderr has no consumer.
	}
	scriptContext, scriptCancel := context.WithCancel(sessionContext)
	defer scriptCancel()
	var scriptDone <-chan struct{}
	if scriptName != "" {
		e.scriptMutex.Lock()
		done := make(chan struct{})
		scriptDone = done
		go e.runLuaScript(scriptContext, scriptName, done)
	} else {
		err = e.registry.EnterInspection(control.InspectionRequest{IsActionSettled: true})
		if err != nil {
			e.logFailure("Scenario inspection transition", err)
		}
	}
	retention := time.NewTimer(control.InspectionRetention + debugLuaExecutionLimit + 5*time.Second)
	if scriptDone == nil {
		retention.Reset(control.InspectionRetention)
	}
	defer retention.Stop()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	isStopping := false
	var serveErr error
	for !isStopping {
		select {
		case err = <-results:
			if !errors.Is(err, http.ErrServerClosed) {
				serveErr = fmt.Errorf("debugHTTP: %w", err)
			}
			isStopping = true
		case <-ctx.Done():
			isStopping = true
		case <-e.stop:
			isStopping = true
		case <-retention.C:
			isStopping = true
		case <-scriptDone:
			scriptDone = nil
			retention.Reset(control.InspectionRetention)
		case name := <-e.scriptRequests:
			done := make(chan struct{})
			scriptDone = done
			retention.Reset(control.InspectionRetention + e.executionLimit + 5*time.Second)
			go e.runLuaScript(scriptContext, name, done)
		}
	}
	scriptCancel()
	if e.isKillRequested.Load() {
		sessionCancel()
	}
	if scriptDone != nil {
		select {
		case <-scriptDone:
		case <-time.After(5 * time.Second):
			e.logFailure("Lua helper shutdown", errors.New("helper did not settle within the shutdown deadline"))
		}
	}
	err = e.registry.BeginStop()
	if err != nil {
		e.logFailure("Scenario stop", err)
	}
	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	cleanupErr := e.cleanupDebugOperations(shutdownContext)
	sessionCancel()
	if cleanupErr != nil {
		e.logFailure("Scenario owned cleanup", cleanupErr)
	}
	httpContext, httpCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer httpCancel()
	err = server.Shutdown(httpContext)
	if err != nil {
		closeErr := server.Close()
		return fmt.Errorf("debugShutdown: %w", errors.Join(err, closeErr))
	}
	err = e.registry.CompleteStop()
	if err != nil {
		return fmt.Errorf("debugStopped: %w", err)
	}
	if cleanupErr != nil || serveErr != nil {
		return fmt.Errorf("debugCleanup: %w", errors.Join(cleanupErr, serveErr))
	}
	return nil
}

func (e *debugSupervisor) watchLauncherInput() {
	buffer := make([]byte, 1)
	count, err := os.Stdin.Read(buffer)
	if err != nil || count != 0 {
		e.stopOnce.Do(e.requestStop)
		return
	}
	e.stopOnce.Do(e.requestStop)
}

func (e *debugSupervisor) runLuaScript(ctx context.Context, name string, done chan<- struct{}) {
	defer close(done)
	defer e.scriptMutex.Unlock()
	scriptContext := e.scriptControl.begin(ctx)
	defer e.scriptControl.finish()
	e.prepareScript(scriptContext, name)
}

func serveDebugHTTP(server *http.Server, listener net.Listener, results chan<- error) {
	results <- server.Serve(listener)
}

func (e *debugSupervisor) removeDiscovery(path string) {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		count, outputErr := fmt.Fprintln(os.Stderr, "Remove scenario discovery:", err)
		if outputErr != nil || count == 0 {
			// Cleanup failure cannot be reported when stderr is unavailable.
		}
	}
}

func writeDebugJSON(path string, document any) error {
	payload, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("debugMarshal: %w", err)
	}
	err = os.WriteFile(path, append(payload, '\n'), 0600)
	if err != nil {
		return fmt.Errorf("debugWrite: %w", err)
	}
	return nil
}

func (e *debugSupervisor) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if req.Host != e.authority {
		http.Error(w, "unexpected scenario API authority", http.StatusForbidden)
		return
	}
	if req.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(req.Header.Get("Authorization")), []byte("Bearer "+e.token)) != 1 {
		http.Error(w, "authenticated local scenario access required", http.StatusUnauthorized)
		return
	}
	switch req.Method + " " + req.URL.Path {
	case "POST " + debugAPIPrefix + "/kill":
		e.isKillRequested.Store(true)
		e.cancelSession()
		e.respond(w, http.StatusAccepted, map[string]string{"status": "kill_requested"})
		e.stopOnce.Do(e.requestStop)
	case "POST " + debugAPIPrefix + "/scripts/run":
		e.serveScriptRun(w, req)
	case "POST " + debugAPIPrefix + "/scripts/stop":
		e.scriptControl.stop()
		e.respond(w, http.StatusAccepted, map[string]string{"status": "script_stop_requested"})
	case "GET " + debugAPIPrefix + "/capabilities":
		e.respond(w, http.StatusOK, map[string]any{"schema": 1, "is_scenario": true,
			"script_stop": map[string]string{"method": "POST", "path": debugAPIPrefix + "/scripts/stop", "scope": "Lua script only; clients and server remain running"},
			"available":   []string{"status", "script_source_inventory", "lua_source_execution", "scenario.sleep", "scenario.log", "server.start", "profile.create", "profile.reset", "profile.logout", "profile.wait_login", "profile.state", "profile.wait_location", "login_observation", "client.launch", "client.state", "client.window", "client.open_creature_editor", "client.ui_hierarchy", "client.click", "client.press", "client.type", "client.ready", "client.wait_scene", "client.set_scene", "client.mission_state", "client.wait_mission", "client.wait_deployment", "client.mission_selection", "client.wait_selection", "client.select_mission", "client.deployment_state", "client.wait_deployment_ready", "client.start_mission", "client.continue_mission", "client.beam_out", "client.close", "client.screenshot", "client.capture", "client.wait_signal", "client.screen_state", "client.set_screen", "client.wait_screen", "client.world_state", "client.world_living", "client.wait_world_ready", "client.enter_mission", "client.world_path", "client.configure_world", "client.spawn_npc", "client.spawn_npc_async", "client.spawn_state", "client.spawn_controlled_npc", "client.spawn_controlled_fixture", "client.spawn_owned_npc", "client.activate_spoffit_crystal", "client.deactivate_spoffit_crystal", "client.controlled_npc_state", "client.controlled_npc_set_ai", "client.controlled_npc_move", "client.controlled_npc_cast", "client.controlled_npc_restore", "client.native_state", "client.project_world", "client.click_world", "client.hold_world", "client.select_hero", "client.restore_hero", "client.replace_hero", "client.place_hero", "client.reset_cooldowns", "client.set_ability_rank", "client.ability_state", "client.cast_active", "client.cancel_ability", "automation.state", "automation.pause", "automation.resume", "cinematic.configure", "cinematic.snapshot", "pvp.start", "client.arena_lobby", "client.accept_arena", "client.wait_arena", "party.create", "party.invite", "party.join", "party.state", "client.debug_pause", "client.debug_resume", "client.debug_pause_state", "graceful_shutdown"},
			"unavailable": []string{"generic_menu_actions", "native_party_input", "generic_gameplay_actions", "simulation_pause", "remote_client_control"}})
	case "GET " + debugAPIPrefix + "/status":
		e.refreshDebugClients()
		e.respond(w, http.StatusOK, e.statusDTO())
	case "GET " + debugAPIPrefix + "/clients":
		e.refreshDebugClients()
		e.respond(w, http.StatusOK, debugStatusDTO(e.registry.Snapshot()).Clients)
	case "GET " + debugAPIPrefix + "/scripts":
		scripts, err := listDebugScripts(e.scriptRoot)
		if err != nil {
			e.respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		e.respond(w, http.StatusOK, scripts)
	case "POST " + debugAPIPrefix + "/operation":
		e.serveDebugOperation(w, req)
	case "POST " + debugAPIPrefix + "/shutdown":
		e.respond(w, http.StatusAccepted, map[string]string{"status": "shutdown_requested"})
		controller := http.NewResponseController(w)
		err := controller.Flush()
		if err != nil {
			e.logFailure("Scenario shutdown acknowledgement", err)
		}
		e.stopOnce.Do(e.requestStop)
	default:
		http.NotFound(w, req)
	}
}

func (e *debugSupervisor) serveDebugOperation(w http.ResponseWriter, req *http.Request) {
	payload, err := io.ReadAll(http.MaxBytesReader(w, req.Body, 64*1024))
	if err != nil {
		e.respond(w, http.StatusBadRequest, debugOperationResponse{Failure: debugErrorDTO(err, 0)})
		return
	}
	var operation debugOperationRequest
	err = decodeDebugJSON(payload, &operation)
	if err != nil {
		e.respond(w, http.StatusBadRequest, debugOperationResponse{Failure: debugErrorDTO(err, 0)})
		return
	}
	// A caller holding the exact per-client pause token may always revoke
	// that lease; it must not depend on the paused script making progress.
	if e.registry.Snapshot().Lease.Owner == control.LeaseScript && !isDebugReadOperation(operation.Operation) && !isDebugAutomationOperation(operation.Operation) && operation.Operation != "client.debug_resume" {
		e.respond(w, http.StatusConflict, debugOperationResponse{Failure: &debugError{Code: "conflict", Context: "scriptLease", Message: "the running script owns scenario mutations"}})
		return
	}
	result, err := e.executeDebugRequest(req.Context(), operation)
	response := debugOperationResponse{Result: result}
	if err != nil {
		response.Failure = debugErrorDTO(err, 0)
	}
	e.respond(w, http.StatusOK, response)
}

func isDebugReadOperation(operation string) bool {
	switch operation {
	case "client.arena_lobby", "client.wait_arena":
		return true
	case "client.screen_state", "client.wait_screen", "client.ui_hierarchy":
		return true
	case "client.debug_pause_state":
		return true
	case "client.controlled_npc_state":
		return true
	case "client.ability_state", "client.world_state", "client.world_living", "client.npc_catalog", "client.wait_world_ready", "client.world_path", "client.native_state", "client.project_world", "client.spawn_state", "client.capture", "client.screenshot":
		return true
	case "status", "client.state", "client.window", "profile.state", "client.mission_state", "party.state":
		return true
	default:
		return false
	}
}

func (e *debugSupervisor) requestStop() { close(e.stop) }

func (e *debugSupervisor) respond(w http.ResponseWriter, status int, document any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	err := json.NewEncoder(w).Encode(document)
	if err != nil {
		// A disconnected HTTP caller cannot receive an additional error response.
		return
	}
}
