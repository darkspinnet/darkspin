//go:build scenario

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/content"
	"github.com/darkspinnet/darkspin/server/game"
	serverruntime "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/scenario/desktop"
	"github.com/darkspinnet/darkspin/server/snapshot"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

type debugHostRequest struct {
	RunID         string
	Paths         scenario.RunPaths
	GameDirectory string
	ContentPath   string
	FangPath      string
	Port          uint16
	IsModded      bool
}

type debugProfileCredential struct {
	LoginName string
	JWT       string
	UserID    int64
}

// debugHost owns an ordinary local loopback server. It does not
// register a solo mission, force a Continue shell, or infer client readiness.
type debugHost struct {
	server            *serverruntime.Server
	Address           string
	GameDirectory     string
	FangPath          string
	ContentPath       string
	IsModded          bool
	runID             string
	secret            string
	mutex             sync.Mutex
	credentialsByName map[string]debugProfileCredential
	cancel            context.CancelFunc
	done              chan struct{}
	runErr            error
	logOutput         *os.File
	serverLogPath     string
	serverTracePath   string
}

func newDebugHost(ctx context.Context, req debugHostRequest) (*debugHost, error) {
	if ctx == nil {
		return nil, errors.New("debug host requires a session lifetime context")
	}
	err := ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("hostContext: %w", err)
	}
	err = desktop.ValidatePort(req.Port)
	if err != nil {
		return nil, fmt.Errorf("hostPort: %w", err)
	}
	// Each startup attempt owns fresh prescribed directories; profiles remain
	// scoped to the supervisor's session identity across successful script runs.
	startReq := desktop.StartRequest{RunID: req.Paths.RunID, Paths: req.Paths,
		GameDirectory: req.GameDirectory, ContentPath: req.ContentPath, FangPath: req.FangPath, Port: req.Port}
	err = validateScenarioHostPaths(startReq)
	if err != nil {
		return nil, fmt.Errorf("hostPaths: %w", err)
	}
	for _, path := range []string{req.ContentPath, req.FangPath} {
		err = desktop.CheckRegular(path)
		if err != nil {
			return nil, fmt.Errorf("hostInput: %w", err)
		}
	}
	err = desktop.CheckAncestors(req.GameDirectory)
	if err != nil {
		return nil, fmt.Errorf("hostGame: %w", err)
	}
	if !filepath.IsAbs(req.GameDirectory) {
		return nil, errors.New("debug game directory must be absolute")
	}
	gameFI, err := os.Lstat(req.GameDirectory)
	if err != nil {
		return nil, fmt.Errorf("hostGameStat: %w", err)
	}
	if !gameFI.IsDir() {
		return nil, errors.New("debug game path must be a directory")
	}
	startupContext, startupCancel := context.WithTimeout(ctx, 30*time.Second)
	defer startupCancel()
	runtimePath := filepath.Join(req.Paths.CacheDirectory, "server")
	err = os.Mkdir(runtimePath, 0700)
	if err != nil {
		return nil, fmt.Errorf("hostRuntime: %w", err)
	}
	err = os.Mkdir(filepath.Join(runtimePath, "cache"), 0700)
	if err != nil {
		return nil, fmt.Errorf("hostCache: %w", err)
	}
	input, err := snapshotScenarioContent(startupContext, req.ContentPath, filepath.Join(req.Paths.CacheDirectory, "content-input.db"))
	if err != nil {
		return nil, fmt.Errorf("hostSnapshot: %w", err)
	}
	working, err := snapshotScenarioContent(startupContext, input.Path, filepath.Join(runtimePath, "cache", "content.db"))
	if err != nil {
		return nil, fmt.Errorf("hostWorking: %w", err)
	}
	if input.SHA256 != working.SHA256 {
		return nil, errors.New("debug working content digest differs from preserved input")
	}
	err = content.PrepareWeb(startupContext, content.WebOptions{GamePath: req.GameDirectory,
		StaticPath: filepath.Join(runtimePath, "cache", "www", "static")})
	if err != nil {
		return nil, fmt.Errorf("hostWeb: %w", err)
	}
	secret, err := scenarioRandomText()
	if err != nil {
		return nil, fmt.Errorf("hostSecret: %w", err)
	}
	marker, err := scenarioRandomText()
	if err != nil {
		return nil, fmt.Errorf("hostMarker: %w", err)
	}
	config := game.DefaultConfig()
	settings := map[game.ConfigKey]string{
		game.ConfigServerPort:           strconv.Itoa(int(req.Port)),
		game.ConfigIsMultiplayerEnabled: "false", game.ConfigStorageDriver: "sqlite",
		game.ConfigAuthJWTSecret: secret, game.ConfigAuthJWTIssuer: "darkspin-scenario",
		game.ConfigAuthJWTAudience: "darkspin", game.ConfigSnapshotMode: "off",
		game.ConfigIsChatStdoutEnabled: "false",
	}
	for key, setting := range settings {
		err = config.Set(key, setting)
		if err != nil {
			return nil, fmt.Errorf("hostConfig[%s]: %w", key, err)
		}
	}
	configPath := filepath.Join(runtimePath, "darkspin.toml")
	err = config.Save(configPath)
	if err != nil {
		return nil, fmt.Errorf("hostSave: %w", err)
	}
	err = startupContext.Err()
	if err != nil {
		return nil, fmt.Errorf("hostPrepare: %w", err)
	}
	logOutput, err := os.OpenFile(filepath.Join(req.Paths.ReportDirectory, "server.log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("hostLog: %w", err)
	}
	host := &debugHost{runID: req.RunID, secret: secret, GameDirectory: req.GameDirectory,
		serverLogPath: filepath.Join(req.Paths.ReportDirectory, "server.log"), serverTracePath: filepath.Join(req.Paths.TraceDirectory, "server.jsonl"),
		FangPath: req.FangPath, ContentPath: working.Path, IsModded: req.IsModded, Address: net.JoinHostPort("127.0.0.1", strconv.Itoa(int(req.Port))),
		credentialsByName: make(map[string]debugProfileCredential), done: make(chan struct{}), logOutput: logOutput}
	host.server, err = serverruntime.New(serverruntime.Options{
		GamePath: req.GameDirectory, ConfigPath: configPath, RuntimePath: runtimePath,
		IsModded:   req.IsModded,
		Logger:     log.New(logOutput, "", log.LstdFlags|log.LUTC),
		TracePath:  filepath.Join(req.Paths.TraceDirectory, "server.jsonl"),
		HTTPRoutes: []serverruntime.HTTPRoute{{Pattern: "/scenario/ready", Methods: []string{http.MethodGet}, Handler: scenarioReadyHandler{marker: marker}}},
	})
	if err != nil {
		closeErr := logOutput.Close()
		return nil, fmt.Errorf("hostCreate: %w", errors.Join(err, closeErr))
	}
	err = host.server.ScenarioConfigureCapture(startupContext, snapshot.ScenarioRecorderRequest{
		RunID: req.Paths.RunID, ReportDirectory: req.Paths.ReportDirectory, TraceDirectory: req.Paths.TraceDirectory})
	if err != nil {
		host.server.Close()
		closeErr := logOutput.Close()
		return nil, fmt.Errorf("hostCapture: %w", errors.Join(err, closeErr))
	}
	runContext, cancel := context.WithCancel(ctx)
	host.cancel = cancel
	go host.run(runContext)
	err = host.waitReady(startupContext, marker)
	if err != nil {
		cleanupContext, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		closeErr := host.Close(cleanupContext)
		cleanupCancel()
		return nil, fmt.Errorf("hostReady: %w", errors.Join(err, closeErr))
	}
	return host, nil
}

func (e *debugHost) run(ctx context.Context) {
	runErr := e.server.Run(ctx)
	closeErr := e.logOutput.Close()
	e.runErr = errors.Join(runErr, closeErr)
	close(e.done)
}

func (e *debugHost) waitReady(ctx context.Context, marker string) error {
	// Reuse the bounded marker probe without the old profile/mission lifecycle.
	probe := &scenarioHost{Address: e.Address, done: e.done}
	client := &http.Client{Timeout: 500 * time.Millisecond, Transport: &http.Transport{Proxy: nil}, CheckRedirect: rejectScenarioReadyRedirect}
	defer client.CloseIdleConnections()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := probe.checkReady(ctx, client, marker)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("hostReadyContext: %w", ctx.Err())
		case <-e.done:
			if e.runErr != nil {
				return fmt.Errorf("hostReadyExit: %w", e.runErr)
			}
			return errors.New("debug server stopped before readiness")
		case <-ticker.C:
		}
	}
}

func (e *debugHost) CreateProfile(ctx context.Context, req sporenet.DebugProfileRequest) (sporenet.DebugProfile, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	_, isCreated := e.credentialsByName[req.ClientName]
	if isCreated {
		return sporenet.DebugProfile{}, errors.New("client already has a fresh owned profile")
	}
	profile, err := e.createProfile(ctx, req)
	if err != nil {
		return sporenet.DebugProfile{}, fmt.Errorf("profileNew: %w", err)
	}
	return profile, nil
}

func (e *debugHost) HasProfile(name string) bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	credential, isCreated := e.credentialsByName[name]
	return isCreated && credential.UserID != 0
}

func (e *debugHost) LogoutProfile(ctx context.Context, name string) error {
	e.mutex.Lock()
	credential, isCreated := e.credentialsByName[name]
	e.mutex.Unlock()
	if !isCreated || credential.UserID == 0 {
		return errors.New("logout requires an owned profile")
	}
	err := e.server.DebugLogoutProfile(ctx, credential.UserID)
	if err != nil {
		return fmt.Errorf("ownedLogout: %w", err)
	}
	return nil
}

// ResetProfile replaces only the alias with a newly created account. The
// supervisor must first reject reset while a managed client uses this alias.
// No setup is inherited implicitly from the previous account.
func (e *debugHost) ResetProfile(ctx context.Context, req sporenet.DebugProfileRequest) (sporenet.DebugProfile, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	_, isCreated := e.credentialsByName[req.ClientName]
	if !isCreated {
		return sporenet.DebugProfile{}, errors.New("client has no fresh owned profile to reset")
	}
	profile, err := e.createProfile(ctx, req)
	if err != nil {
		return sporenet.DebugProfile{}, fmt.Errorf("profileReset: %w", err)
	}
	return profile, nil
}

// createProfile requires e.mutex. Publishing the alias is the final step, after
// the fresh account and launch credential both succeed; errors retain its old
// mapping. The feature operation never updates an existing stored account.
func (e *debugHost) createProfile(ctx context.Context, req sporenet.DebugProfileRequest) (sporenet.DebugProfile, error) {
	if ctx == nil {
		return sporenet.DebugProfile{}, errors.New("profile creation requires a context")
	}
	err := ctx.Err()
	if err != nil {
		return sporenet.DebugProfile{}, fmt.Errorf("profileContext: %w", err)
	}
	if req.RunID != e.runID {
		return sporenet.DebugProfile{}, errors.New("profile run identity does not match the owned host")
	}
	select {
	case <-e.done:
		return sporenet.DebugProfile{}, errors.New("owned debug server has stopped")
	default:
	}
	password, err := scenarioRandomText()
	if err != nil {
		return sporenet.DebugProfile{}, fmt.Errorf("profilePassword: %w", err)
	}
	// Credentials are generated by the owner, never accepted from an API caller.
	req.Password = password
	profile, err := e.server.DebugCreateProfile(ctx, req)
	if err != nil {
		return sporenet.DebugProfile{}, fmt.Errorf("profileCreate: %w", err)
	}
	jwt, err := issueScenarioJWT(e.secret, profile.LoginName)
	if err != nil {
		return sporenet.DebugProfile{}, fmt.Errorf("profileToken: %w", err)
	}
	e.credentialsByName[req.ClientName] = debugProfileCredential{LoginName: profile.LoginName, JWT: jwt, UserID: profile.UserID}
	return profile, nil
}

func (e *debugHost) ProfileCredential(name string) (debugProfileCredential, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	credential, isCreated := e.credentialsByName[name]
	if !isCreated {
		return debugProfileCredential{}, errors.New("client has no fresh owned profile")
	}
	select {
	case <-e.done:
		return debugProfileCredential{}, errors.New("owned debug server has stopped")
	default:
	}
	// Inspection outlives a launch token; issue fresh credentials at launch time.
	jwt, err := issueScenarioJWT(e.secret, credential.LoginName)
	if err != nil {
		return debugProfileCredential{}, fmt.Errorf("profileRefresh: %w", err)
	}
	credential.JWT = jwt
	e.credentialsByName[name] = credential
	return credential, nil
}

func (e *debugHost) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("debug host close requires a context")
	}
	e.cancel()
	select {
	case <-e.done:
		if e.runErr != nil {
			return fmt.Errorf("hostStop: %w", e.runErr)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("hostClose: %w", ctx.Err())
	}
}
