//go:build scenario

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/scenario/desktop"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

type debugOperationState struct {
	host           *debugHost
	clientManager  *debugClientManager
	cacheDirectory string
	serverRequest  debugServerStartRequest
}

type debugServerStartRequest struct {
	IsModded      bool   `json:"is_modded"`
	GameDirectory string `json:"game_directory"`
	ContentPath   string `json:"content_path"`
	FangPath      string `json:"fang_path"`
}

type debugProfileCreateRequest struct {
	Name                 string   `json:"name"`
	HeroNouns            []uint32 `json:"hero_nouns"`
	ChainProgression     uint32   `json:"chain_progression"`
	SourceLoginName      string   `json:"source_login_name"`
	IsSourceCloneAllowed bool     `json:"is_source_clone_allowed"`
	IsPVPEnabled         bool     `json:"is_pvp_enabled"`
}

type debugLaunchRequest struct {
	IsModded           bool   `json:"is_modded"`
	SporePath          string `json:"spore_path,omitempty"`
	ClientID           string `json:"client_id"`
	ProfileName        string `json:"profile_name"`
	IsDetached         bool   `json:"is_detached"`
	LaunchMode         string `json:"launch_mode,omitempty"`
	IsCinematicSkipped *bool  `json:"is_cinematic_skipped,omitempty"`
	IsIntroSkipped     *bool  `json:"is_intro_skipped,omitempty"`
}

type debugClientRequest struct {
	ClientID    string `json:"client_id"`
	IsTerminate bool   `json:"is_terminate"`
}

type debugLoginRequest struct {
	Name      string `json:"name"`
	TimeoutMS uint32 `json:"timeout_ms"`
}

type debugScreenshotRequest struct {
	ClientID string `json:"client_id"`
	Label    string `json:"label"`
}

func waitDebugLogin(ctx context.Context, host *debugHost, req debugLoginRequest) (any, error) {
	if req.TimeoutMS == 0 || req.TimeoutMS > 120000 {
		return nil, errors.New("login timeout must be between 1 and 120000 milliseconds")
	}
	credential, err := host.ProfileCredential(req.Name)
	if err != nil {
		return nil, fmt.Errorf("loginProfile: %w", err)
	}
	observationContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		observation, observeErr := host.server.ScenarioObservation(observationContext, credential.UserID, scenario.Authenticated)
		if observeErr != nil {
			return nil, fmt.Errorf("loginObserve: %w", observeErr)
		}
		if observation.Outcome == scenario.Passed {
			return map[string]any{"name": req.Name, "is_authenticated": true, "session_id": observation.SessionID}, nil
		}
		select {
		case <-observationContext.Done():
			return nil, fmt.Errorf("loginDeadline: %w", observationContext.Err())
		case <-ticker.C:
		}
	}
}

type debugOwnedClientDTO struct {
	IsModded    bool        `json:"is_modded"`
	UserID      int64       `json:"user_id"`
	ClientID    string      `json:"client_id"`
	ProfileName string      `json:"profile_name"`
	InstanceID  string      `json:"instance_id"`
	PID         uint32      `json:"pid"`
	IsDetached  bool        `json:"is_detached"`
	IsRunning   bool        `json:"is_running"`
	LaunchMode  string      `json:"launch_mode"`
	Status      string      `json:"status"`
	Observation debugClient `json:"observation"`
}

func debugOwnedClientResponse(client debugClientState) debugOwnedClientDTO {
	return debugOwnedClientDTO{UserID: client.UserID, ClientID: client.ClientID, ProfileName: client.ProfileName, InstanceID: client.InstanceID, PID: client.ProcessID,
		IsModded: client.IsModded, IsDetached: client.IsDetached, IsRunning: client.IsRunning, LaunchMode: client.LaunchMode, Status: client.Status, Observation: debugClientDTO(client.Observation)}
}

func decodeDebugPayload(payload json.RawMessage, req any) error {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(req)
	if err != nil {
		return fmt.Errorf("payloadDecode: %w", err)
	}
	var trailing any
	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		return errors.New("payload contains trailing JSON")
	}
	return nil
}

func (e *debugSupervisor) executeDebugOperation(ctx context.Context, operation string, payload json.RawMessage) (any, error) {
	if isIndependentDebugOperation(operation) {
		result, err := e.executeIndependentDebugOperation(ctx, operation, payload)
		if err != nil {
			return result, fmt.Errorf("independentOperation: %w", err)
		}
		return result, nil
	}
	err := e.lockDebugOperations(ctx)
	if err != nil {
		return nil, fmt.Errorf("operationLock: %w", err)
	}
	defer e.operationMutex.Unlock()
	if e.operationState == nil {
		e.operationState = &debugOperationState{cacheDirectory: filepath.Join(debugWorkspaceRoot(e.gameDirectory), "bin", "cache", "scenario", e.id)}
	}
	state := e.operationState
	switch operation {
	case "scenario.log":
		var req struct {
			Message string `json:"message"`
		}
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("logRequest: %w", err)
		}
		message := strings.TrimSpace(req.Message)
		if message == "" || len(message) > 2048 || strings.ContainsAny(message, "\r\n") {
			return nil, errors.New("scenario log message must be one nonempty line of at most 2048 bytes")
		}
		e.logLuaActivity(message)
		return nil, nil
	case "party.create", "party.invite", "party.join":
		result, err := executeDebugPartyOperation(ctx, state.host, state.clientManager, operation, payload)
		if err != nil {
			return result, fmt.Errorf("partyOperation: %w", err)
		}
		return result, nil
	case "status":
		e.publishDebugClients()
		return e.statusDTO(), nil
	case "server.start":
		var req debugServerStartRequest
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("serverRequest: %w", err)
		}
		if req.GameDirectory == "" {
			req.GameDirectory = e.gameDirectory
		}
		if req.ContentPath == "" {
			req.ContentPath = filepath.Join(req.GameDirectory, "darkspin", "cache", "content.db")
		}
		if req.FangPath == "" {
			req.FangPath = filepath.Join(req.GameDirectory, "fang.dll")
		}
		if state.host != nil {
			previous := state.serverRequest
			if previous.IsModded != req.IsModded || !strings.EqualFold(filepath.Clean(previous.GameDirectory), filepath.Clean(req.GameDirectory)) || !strings.EqualFold(filepath.Clean(previous.ContentPath), filepath.Clean(req.ContentPath)) || !strings.EqualFold(filepath.Clean(previous.FangPath), filepath.Clean(req.FangPath)) {
				return nil, errors.New("server configuration differs from this session; stop the session before changing server inputs")
			}
			select {
			case <-state.host.done:
				return nil, errors.New("session server has stopped; stop the session before starting again")
			default:
			}
			e.logLuaActivity("Reusing session server")
			return map[string]any{"address": state.host.Address, "is_ready": true, "is_modded": state.host.IsModded}, nil
		}
		attempt, err := scenarioRandomText()
		if err != nil {
			return nil, fmt.Errorf("serverAttempt: %w", err)
		}
		attemptName := e.id + "-server-" + attempt[:24]
		paths := scenario.RunPaths{RunID: attemptName, CacheDirectory: filepath.Join(filepath.Dir(state.cacheDirectory), attemptName), ReportDirectory: filepath.Join(filepath.Dir(e.reportDirectory), attemptName),
			TraceDirectory: filepath.Join(debugWorkspaceRoot(e.gameDirectory), "bin", "server", "darkspin", "logs", "traces", attemptName)}
		for _, directory := range []string{paths.CacheDirectory, paths.ReportDirectory, paths.TraceDirectory} {
			err = os.MkdirAll(directory, 0700)
			if err != nil {
				return nil, fmt.Errorf("serverDirectory: %w", err)
			}
		}
		state.host, err = newDebugHost(e.sessionContext, debugHostRequest{RunID: e.id, Paths: paths, GameDirectory: req.GameDirectory, ContentPath: req.ContentPath, FangPath: req.FangPath, Port: desktop.CanonicalServerPort, IsModded: req.IsModded})
		if err != nil {
			return nil, fmt.Errorf("serverStart: %w", err)
		}
		state.clientManager = newDebugClientManager(e.sessionContext, e.id, state.cacheDirectory, e.reportDirectory)
		state.serverRequest = req
		return map[string]any{"address": state.host.Address, "is_ready": true, "is_modded": state.host.IsModded}, nil
	case "profile.create", "profile.reset":
		if state.host == nil {
			return nil, errors.New("start the isolated server before creating profiles")
		}
		var req debugProfileCreateRequest
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("profileRequest: %w", err)
		}
		var heroNouns [3]uint32
		if req.SourceLoginName == "" {
			if len(req.HeroNouns) != len(heroNouns) {
				return nil, errors.New("profile seed requires exactly three hero nouns")
			}
			copy(heroNouns[:], req.HeroNouns)
		} else if len(req.HeroNouns) != 0 {
			return nil, errors.New("source cloning cannot also supply hero nouns")
		}
		profileReq := sporenet.DebugProfileRequest{RunID: e.id, ClientName: req.Name, HeroNouns: heroNouns, ChainProgression: req.ChainProgression, SourceLoginName: req.SourceLoginName, IsSourceCloneAllowed: req.IsSourceCloneAllowed, IsPVPEnabled: req.IsPVPEnabled}
		var profile sporenet.DebugProfile
		if operation == "profile.reset" || state.host.HasProfile(req.Name) {
			if state.clientManager == nil {
				return nil, errors.New("client manager is not initialized")
			}
			err = e.releaseProfileClients(ctx, req.Name)
			if err != nil {
				return nil, fmt.Errorf("profileRelease: %w", err)
			}
			profile, err = state.host.ResetProfile(ctx, profileReq)
		} else {
			profile, err = state.host.CreateProfile(ctx, profileReq)
		}
		if err != nil {
			return nil, fmt.Errorf("profileSetup: %w", err)
		}
		return map[string]any{"name": req.Name, "login_name": profile.LoginName, "user_id": profile.UserID, "hero_nouns": profile.HeroNouns, "squad_id": profile.ActualSquadID, "is_pvp_enabled": profile.IsPVPEnabled, "pvp_squad_id": profile.ActualPVPSquadID}, nil
	case "client.launch":
		if state.host == nil || state.clientManager == nil {
			return nil, errors.New("start the isolated server before launching clients")
		}
		var req debugLaunchRequest
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("launchRequest: %w", err)
		}
		switch req.LaunchMode {
		case "", "normal":
			if req.LaunchMode == "normal" && req.IsDetached {
				return nil, errors.New("normal launch mode conflicts with is_detached")
			}
		case "detached":
			req.IsDetached = true
		case "remote":
			return nil, errors.New("remote launch mode is reserved; remote guest process control is unavailable")
		default:
			return nil, fmt.Errorf("unsupported client launch mode %q", req.LaunchMode)
		}
		credential, err := state.host.ProfileCredential(req.ProfileName)
		if err != nil {
			return nil, fmt.Errorf("launchProfile: %w", err)
		}
		isCinematicSkipped := true
		if req.IsCinematicSkipped != nil {
			isCinematicSkipped = *req.IsCinematicSkipped
		}
		isIntroSkipped := true
		if req.IsIntroSkipped != nil {
			isIntroSkipped = *req.IsIntroSkipped
		}
		if req.IsModded != state.host.IsModded {
			return nil, errors.New("client is_modded must match the owned server's is_modded option")
		}
		if !req.IsModded && req.SporePath != "" {
			return nil, errors.New("spore_path requires a mod client")
		}
		manifestPath := ""
		if req.IsModded {
			manifestPath, err = prepareDebugMod(ctx, state.host, state.cacheDirectory, req)
			if err != nil {
				return nil, fmt.Errorf("launchMod: %w", err)
			}
		}
		snapshotEndpoint, snapshotToken, err := state.host.server.SnapshotControlForUser(credential.UserID)
		if err != nil {
			return nil, fmt.Errorf("snapshotControl: %w", err)
		}
		client, err := state.clientManager.Launch(ctx, debugClientLaunchRequest{UserID: credential.UserID, ClientID: req.ClientID, ProfileName: req.ProfileName, GameDirectory: state.host.GameDirectory, FangPath: state.host.FangPath, ServerAddress: state.host.Address, JWT: credential.JWT, SnapshotEndpoint: snapshotEndpoint, SnapshotToken: snapshotToken, SnapshotDirectory: state.host.server.SnapshotControlDirectory(), IsDetached: req.IsDetached, IsModded: req.IsModded, ModManifest: manifestPath, IsCinematicSkipped: isCinematicSkipped, IsIntroSkipped: isIntroSkipped})
		if err != nil {
			if client.ProcessID != 0 && client.Observation.Identity.PID != 0 {
				registerErr := e.registry.RegisterClient(client.Observation.Identity)
				if registerErr != nil {
					return debugOwnedClientResponse(client), fmt.Errorf("retainedRegister: %w", errors.Join(err, registerErr))
				}
			}
			return debugOwnedClientResponse(client), fmt.Errorf("clientLaunch: %w", err)
		}
		err = e.registry.RegisterClient(client.Observation.Identity)
		if err != nil {
			closeErr := state.clientManager.Close(ctx, req.ClientID, true)
			return nil, fmt.Errorf("clientRegister: %w", errors.Join(err, closeErr))
		}
		return debugOwnedClientResponse(client), nil
	case "client.close":
		if state.clientManager == nil {
			return nil, errors.New("client manager is not initialized")
		}
		var req debugClientRequest
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("clientRequest: %w", err)
		}
		err = state.clientManager.Close(ctx, req.ClientID, req.IsTerminate)
		if err != nil {
			return nil, fmt.Errorf("clientClose: %w", err)
		}
		return map[string]bool{"is_closed": true}, nil
	default:
		return nil, fmt.Errorf("unsupported debug operation %q", operation)
	}
}

// The operation mutex protects the manager reference. Metadata refresh does not
// wait behind a long-running Lua operation; existing snapshots age to stale.
func (e *debugSupervisor) refreshDebugClients() {
	if !e.operationMutex.TryLock() {
		return
	}
	defer e.operationMutex.Unlock()
	e.publishDebugClients()
}

func (e *debugSupervisor) publishDebugClients() {
	if e.operationState == nil || e.operationState.clientManager == nil {
		return
	}
	for _, client := range e.operationState.clientManager.List() {
		previous, err := e.registry.Client(client.ClientID)
		if err != nil {
			// A partially launched client may not yet have a registered identity.
			continue
		}
		if client.Observation.Sequence <= previous.Sequence {
			continue
		}
		err = e.registry.PublishState(client.Observation)
		if err != nil {
			e.logFailure("Scenario client observation", err)
		}
	}
}

func (e *debugSupervisor) cleanupDebugOperations(ctx context.Context) error {
	lockErr := e.lockDebugOperations(ctx)
	if lockErr != nil {
		return fmt.Errorf("cleanupLock: %w", lockErr)
	}
	defer e.operationMutex.Unlock()
	if e.operationState == nil {
		return nil
	}
	var clientErr, hostErr error
	if e.operationState.clientManager != nil {
		if e.isKillRequested.Load() {
			clientErr = e.operationState.clientManager.Terminate(ctx)
		} else {
			clientErr = e.operationState.clientManager.Shutdown(ctx)
		}
	}
	if e.operationState.host != nil {
		hostErr = e.operationState.host.Close(ctx)
	}
	err := errors.Join(clientErr, hostErr)
	if err != nil {
		return fmt.Errorf("sessionCleanup: %w", err)
	}
	return nil
}

func (e *debugSupervisor) lockDebugOperations(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return fmt.Errorf("operationContext: %w", ctx.Err())
		}
		if e.operationMutex.TryLock() {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("operationWait: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
