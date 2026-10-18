//go:build scenario

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	serverauth "github.com/darkspinnet/darkspin/server/auth"
	"github.com/darkspinnet/darkspin/server/game"
	serverruntime "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/scenario/desktop"
)

type scenarioHost struct {
	runID                  string
	JWT                    string
	Address                string
	ContentArtifact        scenario.Artifact
	ProfileArtifact        scenario.Artifact
	WebArtifact            scenario.Artifact
	server                 *serverruntime.Server
	cancel                 context.CancelFunc
	done                   chan struct{}
	runErr                 error
	logOutput              *os.File
	userID                 int64
	reportDirectory        string
	observationID          uint64
	authenticatedSessionID string
	definition             scenario.Definition
	clientLivenessReader   scenarioClientLivenessReader
	entryCollection        scenarioEntryCollection
}

type scenarioReadyHandler struct{ marker string }

func (e scenarioReadyHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Cache-Control", "no-store")
	count, err := w.Write([]byte(e.marker))
	if err != nil || count != len(e.marker) {
		// Readiness is proved by the caller receiving the full marker. An
		// interrupted response cannot grant readiness.
		return
	}
}

// ctx owns server lifetime. A separate child bounds startup; the worker's job
// containment bounds the existing non-interruptible runtime.New constructor.
func newScenarioHost(ctx context.Context, req desktop.StartRequest) (*scenarioHost, error) {
	if ctx == nil {
		return nil, errors.New("scenario host requires a lifetime context")
	}
	err := desktop.ValidatePort(req.Port)
	if err != nil {
		return nil, fmt.Errorf("hostPort: %w", err)
	}
	err = req.Definition.Validate()
	if err != nil {
		return nil, fmt.Errorf("hostDefinition: %w", err)
	}
	startupContext, startupCancel := context.WithTimeout(ctx, 15*time.Second)
	defer startupCancel()
	err = validateScenarioHostPaths(req)
	if err != nil {
		return nil, fmt.Errorf("hostPaths: %w", err)
	}
	runtimePath := filepath.Join(req.Paths.CacheDirectory, "server")
	err = os.Mkdir(runtimePath, 0700)
	if err != nil {
		return nil, fmt.Errorf("runtimeCreate: %w", err)
	}
	err = os.Mkdir(filepath.Join(runtimePath, "cache"), 0700)
	if err != nil {
		return nil, fmt.Errorf("contentDirectory: %w", err)
	}
	artifact, err := snapshotScenarioContent(startupContext, req.ContentPath,
		filepath.Join(req.Paths.CacheDirectory, "content-input.db"))
	if err != nil {
		return nil, fmt.Errorf("contentSnapshot: %w", err)
	}
	workingArtifact, err := snapshotScenarioContent(startupContext, artifact.Path,
		filepath.Join(runtimePath, "cache", "content.db"))
	if err != nil {
		return nil, fmt.Errorf("contentWorking: %w", err)
	}
	if workingArtifact.SHA256 != artifact.SHA256 {
		return nil, errors.New("scenario working content digest differs from preserved input")
	}
	webArtifact, err := prepareScenarioWeb(startupContext, req, runtimePath)
	if err != nil {
		return nil, fmt.Errorf("serverWeb: %w", err)
	}
	secret, err := scenarioRandomText()
	if err != nil {
		return nil, fmt.Errorf("authRandom: %w", err)
	}
	marker, err := scenarioRandomText()
	if err != nil {
		return nil, fmt.Errorf("readyRandom: %w", err)
	}
	config := game.DefaultConfig()
	settings := map[game.ConfigKey]string{
		game.ConfigServerPort:           strconv.Itoa(int(req.Port)),
		game.ConfigIsMultiplayerEnabled: "false", game.ConfigStorageDriver: "sqlite",
		game.ConfigAuthJWTSecret: secret, game.ConfigAuthJWTIssuer: "darkspin-scenario",
		game.ConfigAuthJWTAudience: "darkspin", game.ConfigSnapshotMode: "off",
		game.ConfigIsChatStdoutEnabled: "false",
		game.ConfigChatFilePath:        filepath.Join(req.Paths.ReportDirectory, "chat.log"),
	}
	for key, setting := range settings {
		err = config.Set(key, setting)
		if err != nil {
			return nil, fmt.Errorf("configSet[%s]: %w", key, err)
		}
	}
	configPath := filepath.Join(runtimePath, "darkspin.toml")
	err = config.Save(configPath)
	if err != nil {
		return nil, fmt.Errorf("configSave: %w", err)
	}
	err = startupContext.Err()
	if err != nil {
		return nil, fmt.Errorf("startupPrepare: %w", err)
	}
	logOutput, err := os.OpenFile(filepath.Join(req.Paths.ReportDirectory, "server.log"),
		os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("logCreate: %w", err)
	}
	host := &scenarioHost{runID: req.RunID, ContentArtifact: artifact, WebArtifact: webArtifact,
		logOutput: logOutput, definition: req.Definition,
		Address:         net.JoinHostPort("127.0.0.1", strconv.Itoa(int(req.Port))),
		reportDirectory: req.Paths.ReportDirectory, done: make(chan struct{})}
	host.server, err = serverruntime.New(serverruntime.Options{
		GamePath: req.GameDirectory, ConfigPath: configPath, RuntimePath: runtimePath,
		Logger:    log.New(logOutput, "", log.LstdFlags|log.LUTC),
		TracePath: filepath.Join(req.Paths.TraceDirectory, "server.jsonl"),
		HTTPRoutes: []serverruntime.HTTPRoute{{Pattern: "/scenario/ready",
			Methods: []string{http.MethodGet}, Handler: scenarioReadyHandler{marker: marker}}},
	})
	if err != nil {
		closeErr := logOutput.Close()
		return nil, fmt.Errorf("serverCreate: %w", errors.Join(err, closeErr))
	}
	err = host.configureCapture(startupContext, req)
	if err != nil {
		host.server.Close()
		closeErr := logOutput.Close()
		return nil, fmt.Errorf("serverCapture: %w", errors.Join(err, closeErr))
	}
	err = startupContext.Err()
	if err != nil {
		host.server.Close()
		closeErr := logOutput.Close()
		return nil, fmt.Errorf("startupContext: %w", errors.Join(err, closeErr))
	}
	password, err := scenarioRandomText()
	if err != nil {
		host.server.Close()
		closeErr := logOutput.Close()
		return nil, fmt.Errorf("passwordRandom: %w", errors.Join(err, closeErr))
	}
	loginName := req.RunID + "@scenario.invalid"
	profile, err := host.server.ScenarioRegister(startupContext, loginName, password, req.RunID, req.Definition)
	if err != nil {
		host.server.Close()
		closeErr := logOutput.Close()
		return nil, fmt.Errorf("profileRegister: %w", errors.Join(err, closeErr))
	}
	host.userID = profile.UserID
	host.ProfileArtifact, err = host.persistProfile(profile)
	if err != nil {
		host.server.Close()
		closeErr := logOutput.Close()
		return nil, fmt.Errorf("profileEvidence: %w", errors.Join(err, closeErr))
	}
	host.JWT, err = issueScenarioJWT(secret, loginName)
	if err != nil {
		host.server.Close()
		closeErr := logOutput.Close()
		return nil, fmt.Errorf("tokenIssue: %w", errors.Join(err, closeErr))
	}
	runContext, cancel := context.WithCancel(ctx)
	host.cancel = cancel
	go host.run(runContext)
	err = host.waitReady(startupContext, marker)
	if err != nil {
		cleanupContext, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		closeErr := host.Close(cleanupContext)
		cleanupCancel()
		return nil, fmt.Errorf("serverReady: %w", errors.Join(err, closeErr))
	}
	return host, nil
}

func (e *scenarioHost) run(ctx context.Context) {
	e.runErr = e.server.Run(ctx)
	closeErr := e.logOutput.Close()
	e.runErr = errors.Join(e.runErr, closeErr)
	close(e.done)
}

func (e *scenarioHost) Capability() scenario.Capability { return e.server.ScenarioCapability() }

func (e *scenarioHost) Close(ctx context.Context) error {
	e.cancel()
	select {
	case <-e.done:
		if e.runErr != nil {
			return fmt.Errorf("serverStop: %w", e.runErr)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("serverClose: %w", ctx.Err())
	}
}

func scenarioRandomText() (string, error) {
	secret := make([]byte, 32)
	count, err := rand.Read(secret)
	if err != nil {
		return "", fmt.Errorf("randomRead: %w", err)
	}
	if count != len(secret) {
		return "", errors.New("scenario random source returned short read")
	}
	return hex.EncodeToString(secret), nil
}

func issueScenarioJWT(secret, loginName string) (string, error) {
	issuer, err := serverauth.NewJWTIssuer([]byte(secret), "darkspin-scenario", "darkspin")
	if err != nil {
		return "", fmt.Errorf("issuerCreate: %w", err)
	}
	token, err := issuer.Issue(loginName, 10*time.Minute)
	if err != nil {
		return "", fmt.Errorf("tokenSign: %w", err)
	}
	return token, nil
}
