package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	contentcache "github.com/darkspinnet/darkspin/content"
	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
	server "github.com/darkspinnet/darkspin/server/runtime"
)

type spinnerServiceSet struct {
	cancel         context.CancelFunc
	initializeDone <-chan struct{}
	gameServer     *server.Server
	serverDone     <-chan struct{}
}

type spinnerPathSet struct {
	gamePath       string
	gameBinaryPath string
	runtimePath    string
	logPath        string
	tracePath      string
	userPath       string
	remotePath     string
	fangPath       string
	configPath     string
	contentPath    string
	staticPath     string
}

func resolveSpinnerPaths(basePath string) (*spinnerPathSet, error) {
	if strings.TrimSpace(basePath) == "" {
		return nil, errors.New("base path is empty")
	}
	basePath, err := filepath.Abs(basePath)
	if err != nil {
		return nil, fmt.Errorf("baseResolve: %w", err)
	}
	gamePath := basePath
	runtimePath := filepath.Join(basePath, "darkspin")
	logPath := filepath.Join(runtimePath, "logs")
	cachePath := filepath.Join(runtimePath, "cache")
	pathSet := &spinnerPathSet{
		gamePath:       gamePath,
		gameBinaryPath: filepath.Join(gamePath, "DarksporeBin", "Darkspore.exe"),
		runtimePath:    runtimePath,
		logPath:        logPath,
		tracePath:      filepath.Join(logPath, "traces"),
		userPath:       filepath.Join(runtimePath, "saves", "darkspin.db"),
		remotePath:     filepath.Join(runtimePath, "saves", "remote.db"),
		fangPath:       filepath.Join(cachePath, "fang.dll"),
		configPath:     filepath.Join(basePath, game.DefaultConfigFilename),
		contentPath:    filepath.Join(cachePath, server.ContentDatabaseFilename),
		staticPath:     filepath.Join(cachePath, "www", "static"),
	}
	return pathSet, nil
}

func (a *App) initialize(ctx context.Context, basePath string) {
	a.mu.Lock()
	a.isInitializing = true
	a.mu.Unlock()
	defer a.finishInitialization(ctx)
	pathSet, err := resolveSpinnerPaths(basePath)
	if err != nil {
		a.failSubsystem("Game", fmt.Errorf("pathResolve: %w", err))
		return
	}
	a.startPreparationTiming(pathSet.logPath)
	a.recordPreparationProgress("Runtime", "Preparing launcher runtime", 0)
	err = prepareSpinnerRuntime(pathSet)
	if err != nil {
		a.finishPreparationTiming("Runtime", err.Error(), 0, "failed")
		a.failSubsystem("Game", fmt.Errorf("runtimePrepare: %w", err))
		return
	}
	a.finishPreparationTiming("Runtime", "Preparing launcher runtime", 100, "ready")
	err = a.prepareProfileAvatars(ctx, pathSet)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		a.failSubsystem("Avatar", fmt.Errorf("avatarPrepare: %w", err))
		return
	}
	err = a.prepareProfileStore(ctx, pathSet)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		a.failSubsystem("Profile", fmt.Errorf("profilePrepare: %w", err))
		return
	}
	_, _, err = game.LoadConfig(pathSet.configPath)
	if err != nil {
		a.failSubsystem("Server", fmt.Errorf("configPrepare: %w", err))
		return
	}
	a.recordPreparationProgress("Game", "Preparing game files", 0)
	err = a.prepareGame(pathSet)
	if err != nil {
		a.failSubsystem("Game", fmt.Errorf("gamePrepare: %w", err))
		return
	}
	patchResult := make(chan error, 1)
	contentResult := make(chan error, 1)
	a.setSubsystem("Auth", "Starting with server", false)
	patchCtx := a.beginPatchOperation(ctx)
	go func() {
		patchErr := a.preparePatch(patchCtx, pathSet.gamePath, pathSet.configPath)
		a.finishPatchOperation(patchErr)
		patchResult <- patchErr
	}()
	go func() {
		contentResult <- a.prepareContent(ctx, pathSet)
	}()
	contentErr := <-contentResult
	if contentErr != nil {
		if errors.Is(contentErr, context.Canceled) {
			return
		}
		a.failSubsystem("Content", fmt.Errorf("contentPrepare: %w", contentErr))
		return
	}
	err = ctx.Err()
	if err != nil {
		return
	}
	err = a.startGameServer(ctx, pathSet)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		a.failSubsystem("Server", fmt.Errorf("serverStart: %w", err))
		return
	}
	a.setSubsystem("Auth", "Online", true)
	patchErr := <-patchResult
	if patchErr != nil {
		if errors.Is(patchErr, context.Canceled) {
			return
		}
		a.failSubsystem("Patch", fmt.Errorf("patchPrepare: %w", patchErr))
		return
	}
	a.mu.Lock()
	a.status.State = "ready"
	a.status.Message = "DarkSpinner ready"
	a.updatePlayReadyLocked()
	a.emitStatusLocked()
	a.mu.Unlock()
	a.finishPreparationTiming("Launcher", "Initialization", 100, "ready")
}

func (a *App) prepareProfileStore(ctx context.Context, pathSet *spinnerPathSet) error {
	a.setSubsystem("Profile", "Initializing", false)
	repository, err := a.openProfileRepositoryAt(ctx, pathSet.userPath)
	if err != nil {
		return fmt.Errorf("profileStore: %w", err)
	}
	err = repository.Close()
	if err != nil {
		return fmt.Errorf("profileClose: %w", err)
	}
	a.setSubsystem("Profile", "Ready", true)
	return nil
}

func (a *App) prepareProfileAvatars(ctx context.Context, pathSet *spinnerPathSet) error {
	a.setSubsystem("Avatar", "Recovering Crogenitor photos", false)
	err := contentcache.PrepareProfileAvatars(ctx, contentcache.WebOptions{
		GamePath: filepath.Join(pathSet.gamePath, "DarksporeBin"), StaticPath: pathSet.staticPath,
	})
	if err != nil {
		return fmt.Errorf("avatarCache: %w", err)
	}
	a.setSubsystem("Avatar", "Ready", true)
	return nil
}

func prepareSpinnerRuntime(pathSet *spinnerPathSet) error {
	if pathSet == nil {
		return errors.New("path set is nil")
	}
	directories := []string{
		pathSet.logPath,
		pathSet.tracePath,
		filepath.Dir(pathSet.userPath),
		filepath.Dir(pathSet.contentPath),
	}
	for index, directory := range directories {
		err := os.MkdirAll(directory, 0o755)
		if err != nil {
			return fmt.Errorf("directoryCreate[%d]: %w", index, err)
		}
	}
	return nil
}

func (a *App) prepareGame(pathSet *spinnerPathSet) error {
	err := preparePlatformGame(pathSet)
	if err != nil {
		return fmt.Errorf("platformPrepare: %w", err)
	}
	gameBinaryPath, err := existingAbsoluteFile(pathSet.gameBinaryPath)
	if err != nil {
		return fmt.Errorf("binaryFind: %w", err)
	}
	err = validateGameData(filepath.Dir(gameBinaryPath))
	if err != nil {
		return fmt.Errorf("dataValidate: %w", err)
	}
	_, err = ensureFang(pathSet.fangPath, embeddedFang)
	if err != nil {
		return fmt.Errorf("fangPrepare: %w", err)
	}
	a.mu.Lock()
	a.status.GameDirectory = pathSet.gamePath
	a.status.Game = "Ready to Launch"
	a.status.GameError = ""
	a.status.IsGameReady = true
	a.updatePlayReadyLocked()
	a.emitStatusLocked()
	a.mu.Unlock()
	return nil
}

func (a *App) prepareContent(ctx context.Context, pathSet *spinnerPathSet) error {
	a.setSubsystemProgress("Content", "preparing", "Preparing visual assets", 5)
	err := contentcache.PrepareWeb(ctx, contentcache.WebOptions{
		GamePath: filepath.Join(pathSet.gamePath, "DarksporeBin"), StaticPath: pathSet.staticPath,
	})
	if err != nil {
		return fmt.Errorf("webPrepare: %w", err)
	}
	a.setSubsystemProgress("Content", "preparing", "Checking prepared content", 15)
	_, err = os.Stat(pathSet.contentPath)
	if err == nil {
		_, err = contentsqlite.Verify(ctx, pathSet.contentPath)
		if err == nil {
			a.setSubsystemProgress("Content", "preparing", "Prepared content ready", 100)
			a.setSubsystem("Content", "Ready", true)
			return nil
		}
		removeErr := os.Remove(pathSet.contentPath)
		if removeErr != nil {
			return fmt.Errorf("contentReplace: %w", errors.Join(err, removeErr))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("contentStat: %w", err)
	}
	err = contentsqlite.Build(ctx, contentsqlite.BuildOptions{
		GamePath: filepath.Join(pathSet.gamePath, "DarksporeBin"), DatabasePath: pathSet.contentPath,
		OnProgress: func(progress contentsqlite.BuildProgress) {
			percent := 20
			if progress.Total > 0 {
				percent += progress.Completed * 75 / progress.Total
			}
			a.setSubsystemProgress("Content", "preparing", progress.Phase, percent)
		},
	})
	if err != nil {
		return fmt.Errorf("contentBuild: %w", err)
	}
	a.setSubsystemProgress("Content", "preparing", "Prepared content ready", 100)
	a.setSubsystem("Content", "Ready", true)
	return nil
}

func (a *App) startGameServer(ctx context.Context, pathSet *spinnerPathSet) error {
	a.setSubsystem("Server", "Starting", false)
	logger := log.New(&spinnerLogWriter{app: a, prefix: "Server"}, "", 0)
	gameServer, err := server.New(server.Options{
		GamePath:    pathSet.gamePath,
		ConfigPath:  pathSet.configPath,
		Logger:      logger,
		RuntimePath: pathSet.runtimePath,
		TracePath:   "server.jsonl",
	})
	if err != nil {
		return fmt.Errorf("serverCreate: %w", err)
	}
	a.mu.Lock()
	a.serviceSet.gameServer = gameServer
	serverDone := make(chan struct{})
	a.serviceSet.serverDone = serverDone
	a.mu.Unlock()
	completedProfileCount, reconcileErr := gameServer.CompletePendingTutorialProfiles(ctx)
	if reconcileErr != nil {
		a.log("Starter loadout reconciliation remains queued: " + reconcileErr.Error())
	} else if completedProfileCount != 0 {
		a.log(fmt.Sprintf("Completed %d queued starter loadouts", completedProfileCount))
	}
	go func() {
		defer close(serverDone)
		runErr := gameServer.Run(ctx)
		if runErr != nil {
			a.failSubsystem("Server", fmt.Errorf("run: %w", runErr))
		}
	}()
	serverHost := "127.0.0.1"
	serverPort, err := gameServer.Config().Uint16(game.ConfigServerPort)
	if err != nil {
		return fmt.Errorf("serverPort: %w", err)
	}
	a.mu.Lock()
	if isLoopbackAuthURL(a.authURL) {
		a.authURL = fmt.Sprintf("http://127.0.0.1:%d", serverPort)
	}
	a.mu.Unlock()
	address := net.JoinHostPort(serverHost, fmt.Sprintf("%d", serverPort))
	err = waitForServerListener(ctx, address, serverDone)
	if err != nil {
		return fmt.Errorf("serverReady: %w", err)
	}
	a.mu.Lock()
	a.status.Server = "Online"
	a.status.ServerError = ""
	a.status.IsServerOnline = true
	a.updatePlayReadyLocked()
	a.emitStatusLocked()
	a.mu.Unlock()
	return nil
}

func waitForServerListener(ctx context.Context, address string, serverDone <-chan struct{}) error {
	if ctx == nil {
		return errors.New("nil server context")
	}
	if strings.TrimSpace(address) == "" {
		return errors.New("empty server address")
	}
	readyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	dialer := net.Dialer{Timeout: 250 * time.Millisecond}
	for {
		connection, err := dialer.DialContext(readyCtx, "tcp", address)
		if err == nil {
			closeErr := connection.Close()
			if closeErr != nil {
				return fmt.Errorf("readyClose: %w", closeErr)
			}
			return nil
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-serverDone:
			timer.Stop()
			return errors.New("server stopped before listener became ready")
		case <-readyCtx.Done():
			timer.Stop()
			return fmt.Errorf("readyWait: %w", readyCtx.Err())
		case <-timer.C:
		}
	}
}

func (a *App) preparePatch(ctx context.Context, gamePath, configPath string) error {
	isRestarting, err := a.prepareLauncherUpdate(ctx)
	if err != nil {
		a.log("Launcher update skipped: " + err.Error())
		a.setSubsystem("Patch", "Pending", false)
	}
	if isRestarting {
		return nil
	}
	plan, err := buildModPatchPlan(ctx, gamePath)
	if err != nil {
		return fmt.Errorf("modPatchPlan: %w", err)
	}
	if plan != nil {
		a.setSubsystem("Patch", "Update available", false)
		return nil
	}
	isDue, err := isGameIntegrityVerificationDue(configPath, time.Now())
	if err != nil {
		return fmt.Errorf("integritySchedule: %w", err)
	}
	if !isDue {
		a.setSubsystem("Patch", "Verified recently", true)
		return nil
	}
	a.setSubsystemProgress("Patch", "checking", "Verifying pristine game files", 0)
	err = a.verifyGameIntegrity(ctx, gamePath, "checking")
	if err != nil {
		return fmt.Errorf("integrityVerify: %w", err)
	}
	err = recordGameIntegrityVerification(configPath, time.Now())
	if err != nil {
		return fmt.Errorf("integrityRecord: %w", err)
	}
	a.mu.Lock()
	a.status.Patch = "Integrity verified"
	a.status.PatchProgress = 100
	a.status.PatchError = ""
	a.status.Progress = 100
	a.status.IsPatchComplete = true
	a.updatePlayReadyLocked()
	a.emitStatusLocked()
	a.mu.Unlock()
	return nil
}

func (a *App) beginPatchOperation(ctx context.Context) context.Context {
	patchCtx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	a.patchCancel = cancel
	a.status.IsPatchActive = true
	a.emitStatusLocked()
	a.mu.Unlock()
	return patchCtx
}

func (a *App) finishPatchOperation(err error) {
	a.mu.Lock()
	a.patchCancel = nil
	a.status.IsPatchActive = false
	if errors.Is(err, context.Canceled) {
		a.status.Patch = "Canceled"
		a.status.PatchProgress = 0
		a.status.PatchError = ""
		a.status.IsPatchComplete = false
		a.status.Message = "Patch canceled"
	}
	a.updatePlayReadyLocked()
	a.emitStatusLocked()
	a.mu.Unlock()
}

func (a *App) CancelPatch() {
	a.mu.Lock()
	patchCancel := a.patchCancel
	preparationCancel := context.CancelFunc(nil)
	if a.isInitializing {
		preparationCancel = a.serviceSet.cancel
	}
	isCancelable := patchCancel != nil || preparationCancel != nil
	if isCancelable {
		a.status.State = "cancelling"
		a.status.Patch = "Cancelling"
		a.status.Message = "Cancelling patch"
		a.emitStatusLocked()
	}
	a.mu.Unlock()
	if preparationCancel != nil {
		preparationCancel()
		return
	}
	if patchCancel != nil {
		patchCancel()
	}
}

func (a *App) finishInitialization(ctx context.Context) {
	a.mu.Lock()
	a.isInitializing = false
	if ctx.Err() != nil && a.status.State == "cancelling" {
		a.status.State = "cancelled"
		a.status.Message = "Patch canceled"
		a.status.Patch = "Canceled"
		a.status.PatchProgress = 0
		a.status.PatchError = ""
		a.status.IsPatchComplete = false
		a.status.IsPatchActive = false
		a.updatePlayReadyLocked()
	}
	a.emitStatusLocked()
	a.mu.Unlock()
}

func (a *App) setSubsystem(name, state string, isReady bool) {
	if isReady {
		a.finishPreparationTiming(name, state, 100, "ready")
	} else {
		a.recordPreparationProgress(name, state, 0)
	}
	a.mu.Lock()
	switch name {
	case "Auth":
		a.status.Auth = state
		a.status.IsAuthOnline = isReady
		if isReady {
			a.status.AuthError = ""
		}
	case "Server":
		a.status.Server = state
		a.status.IsServerOnline = isReady
		if isReady {
			a.status.ServerError = ""
		}
	case "Patch":
		a.status.Patch = state
		if isReady {
			a.status.PatchProgress = 100
		} else {
			a.status.PatchProgress = 0
		}
		a.status.IsPatchComplete = isReady
		if isReady {
			a.status.PatchError = ""
		}
	case "Game":
		a.status.Game = state
		a.status.IsGameReady = isReady
		if isReady {
			a.status.GameError = ""
		}
	case "Avatar":
		a.status.Avatar = state
		if isReady {
			a.status.AvatarProgress = 100
		} else {
			a.status.AvatarProgress = 0
		}
		a.status.IsAvatarReady = isReady
		if isReady {
			a.status.AvatarError = ""
		}
	case "Profile":
		a.status.Profile = state
		a.status.IsProfileStoreReady = isReady
		if isReady {
			a.status.ProfileError = ""
		}
	case "Content":
		a.status.Content = state
		if isReady {
			a.status.ContentProgress = 100
		} else {
			a.status.ContentProgress = 0
		}
		a.status.IsContentReady = isReady
		if isReady {
			a.status.ContentError = ""
		}
	}
	a.updatePlayReadyLocked()
	a.emitStatusLocked()
	a.mu.Unlock()
}

func (e *App) setSubsystemProgress(
	name string,
	state string,
	message string,
	progress int,
) {
	e.recordPreparationProgress(name, message, progress)
	e.mu.Lock()
	switch name {
	case "Patch":
		e.status.Patch = message
		e.status.PatchProgress = progress
	case "Avatar":
		e.status.Avatar = message
		e.status.AvatarProgress = progress
	case "Content":
		e.status.Content = message
		e.status.ContentProgress = progress
	}
	e.status.State = state
	e.status.Message = message
	e.status.Progress = progress
	e.updatePlayReadyLocked()
	e.emitStatusLocked()
	e.mu.Unlock()
}

func (a *App) failSubsystem(name string, err error) {
	a.finishPreparationTiming(name, err.Error(), 0, "failed")
	a.finishPreparationTiming("Launcher", err.Error(), 0, "failed")
	a.setCriticalFailure(name, err)
	a.setState("error", err.Error(), 0)
}

func (a *App) setCriticalFailure(name string, err error) {
	a.mu.Lock()
	switch name {
	case "Identity":
		a.status.IdentityError = err.Error()
		a.status.IsAuthenticated = false
	case "Auth":
		a.status.AuthError = err.Error()
		a.status.Auth = "Error"
		a.status.IsAuthOnline = false
	case "Server":
		a.status.ServerError = err.Error()
		a.status.Server = "Error"
		a.status.IsServerOnline = false
	case "Patch":
		a.status.PatchError = err.Error()
		a.status.Patch = "Error"
		a.status.IsPatchComplete = false
	case "Game":
		a.status.GameError = err.Error()
		a.status.Game = "Error"
		a.status.IsGameReady = false
	case "Avatar":
		a.status.AvatarError = err.Error()
		a.status.Avatar = "Error"
		a.status.IsAvatarReady = false
	case "Profile":
		a.status.ProfileError = err.Error()
		a.status.Profile = "Error"
		a.status.IsProfileStoreReady = false
	case "Content":
		a.status.ContentError = err.Error()
		a.status.Content = "Error"
		a.status.IsContentReady = false
	}
	a.updatePlayReadyLocked()
	a.emitStatusLocked()
	a.mu.Unlock()
}

func (a *App) stopServices() {
	a.mu.Lock()
	cancel := a.serviceSet.cancel
	initializeDone := a.serviceSet.initializeDone
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.waitForShutdown("Initialization", initializeDone)

	a.mu.Lock()
	gameServer := a.serviceSet.gameServer
	serverDone := a.serviceSet.serverDone
	a.mu.Unlock()
	if gameServer != nil {
		closeDone := make(chan struct{})
		go func() {
			defer close(closeDone)
			gameServer.Close()
		}()
		a.waitForShutdown("Server close", closeDone)
	}
	a.waitForShutdown("Server", serverDone)
}

func (a *App) waitForShutdown(name string, done <-chan struct{}) {
	if done == nil {
		return
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		a.log(name + " shutdown timed out")
	}
}

type spinnerLogWriter struct {
	app    *App
	prefix string
}

func (w *spinnerLogWriter) Write(contents []byte) (int, error) {
	message := strings.TrimSpace(string(contents))
	if message != "" {
		w.app.log(w.prefix + ": " + message)
	}
	return len(contents), nil
}

var _ io.Writer = (*spinnerLogWriter)(nil)
