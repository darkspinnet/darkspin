package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/patcher"
	appwindow "github.com/darkspinnet/darkspin/window"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const gameProcessName = "Darkspore.exe"

var visibleProfilePattern = regexp.MustCompile(`(?i)\bprofiles?\b`)

type App struct {
	ctx                context.Context
	lifecycleCtx       context.Context
	mu                 sync.Mutex
	serverMu           sync.Mutex
	logMu              sync.Mutex
	timingMu           sync.Mutex
	cancel             context.CancelFunc
	patchCancel        context.CancelFunc
	serviceSet         spinnerServiceSet
	gameDone           <-chan struct{}
	gameCancel         context.CancelFunc
	clientFailuresDone <-chan struct{}
	isShuttingDown     bool
	isInitializing     bool
	arguments          []string
	authURL            string
	token              string
	status             LauncherStatus
	installation       InstallationStatus
	discoveredGameRoot string
	integrationError   string
	startupError       string
	preparationTiming  *preparationTimingRecorder
}

type LauncherStatus struct {
	State               string `json:"state"`
	Message             string `json:"message"`
	Identity            string `json:"identity"`
	Auth                string `json:"auth"`
	Server              string `json:"server"`
	Patch               string `json:"patch"`
	Game                string `json:"game"`
	Avatar              string `json:"avatar"`
	Profile             string `json:"profile"`
	Content             string `json:"content"`
	IdentityError       string `json:"identityError"`
	AuthError           string `json:"authError"`
	ServerError         string `json:"serverError"`
	PatchError          string `json:"patchError"`
	GameError           string `json:"gameError"`
	AvatarError         string `json:"avatarError"`
	ProfileError        string `json:"profileError"`
	ContentError        string `json:"contentError"`
	LastRun             string `json:"lastRun"`
	LauncherNotice      string `json:"launcherNotice"`
	ManifestURL         string `json:"manifestUrl"`
	GameDirectory       string `json:"gameDirectory"`
	Version             string `json:"version"`
	BuildChannel        string `json:"buildChannel"`
	Progress            int    `json:"progress"`
	PatchProgress       int    `json:"patchProgress"`
	AvatarProgress      int    `json:"avatarProgress"`
	ContentProgress     int    `json:"contentProgress"`
	RequiredFile        int    `json:"requiredFile"`
	DeleteFile          int    `json:"deleteFile"`
	DownloadByte        int64  `json:"downloadByte"`
	IsAuthenticated     bool   `json:"isAuthenticated"`
	IsAuthOnline        bool   `json:"isAuthOnline"`
	IsServerOnline      bool   `json:"isServerOnline"`
	IsPatchComplete     bool   `json:"isPatchComplete"`
	IsPatchActive       bool   `json:"isPatchActive"`
	IsGameReady         bool   `json:"isGameReady"`
	IsAvatarReady       bool   `json:"isAvatarReady"`
	IsProfileStoreReady bool   `json:"isProfileStoreReady"`
	IsContentReady      bool   `json:"isContentReady"`
	IsPlayReady         bool   `json:"isPlayReady"`
	IsAutoPlayRequested bool   `json:"isAutoPlayRequested"`
	IsPatchEnabled      bool   `json:"isPatchEnabled"`
	IsCinematicSkipped  bool   `json:"isCinematicSkipped"`
	IsLastRunFailure    bool   `json:"isLastRunFailure"`
	IsStartupBlocked    bool   `json:"isStartupBlocked"`
}

func NewApp(arguments []string) *App {
	identity := launchArgumentValue(arguments, "account")
	return &App{
		arguments: setBooleanLaunchArgument(arguments, "skip-cinematic", false),
		authURL:   authServiceURL,
		status: LauncherStatus{
			State:               "starting",
			Message:             "Starting DarkSpinner",
			Identity:            identity,
			Auth:                "Starting",
			Server:              "Starting",
			Patch:               "Pending",
			Game:                "Checking",
			Avatar:              "Preparing",
			Profile:             "Starting",
			Content:             "Pending",
			LastRun:             "",
			ManifestURL:         strings.TrimSpace(patchManifestURL),
			Version:             Version,
			BuildChannel:        BuildChannel,
			IsAutoPlayRequested: hasLaunchArgument(arguments, "auto-play"),
			IsPatchEnabled:      strings.TrimSpace(patchManifestURL) != "",
			IsCinematicSkipped:  false,
		},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if a.startupError != "" {
		a.mu.Lock()
		a.status.State = "error"
		a.status.Message = "Standard user required"
		a.status.Auth = "Blocked"
		a.status.Server = "Blocked"
		a.status.Patch = "Blocked"
		a.status.Game = "Blocked"
		a.status.Avatar = "Blocked"
		a.status.Profile = "Blocked"
		a.status.Content = "Blocked"
		a.status.LauncherNotice = a.startupError
		a.status.IsStartupBlocked = true
		a.emitStatusLocked()
		a.mu.Unlock()
		return
	}
	if a.integrationError != "" {
		a.status.LauncherNotice = "Integration setup failed: " + a.integrationError
	}
	lifecycleCtx, lifecycleCancel := context.WithCancel(ctx)
	initializeDone := make(chan struct{})
	a.mu.Lock()
	a.lifecycleCtx = lifecycleCtx
	a.serviceSet.cancel = lifecycleCancel
	a.serviceSet.initializeDone = initializeDone
	a.mu.Unlock()
	basePath, err := executableDirectory()
	if err != nil {
		close(initializeDone)
		a.failSubsystem("Game", fmt.Errorf("executableDirectory: %w", err))
		return
	}
	clientFailuresDone := make(chan struct{})
	a.mu.Lock()
	a.clientFailuresDone = clientFailuresDone
	a.mu.Unlock()
	go a.watchClientFailures(lifecycleCtx, basePath, clientFailuresDone)
	installation, gameRoot := inspectInstallation(basePath)
	a.mu.Lock()
	a.installation = installation
	a.discoveredGameRoot = gameRoot
	a.mu.Unlock()
	if !installation.IsLocalReady {
		close(initializeDone)
		a.mu.Lock()
		a.status.State = "installation-required"
		a.status.Message = installation.Message
		a.status.Game = "Installation required"
		a.status.GameError = ""
		a.emitStatusLocked()
		a.mu.Unlock()
		return
	}
	gamePath := defaultGameExecutable(basePath)
	_, gameWorkingPath, resolveErr := resolveGameExecutable(gamePath, basePath)
	if resolveErr == nil {
		a.mu.Lock()
		a.status.GameDirectory = installDirectory(gameWorkingPath)
		a.mu.Unlock()
	}
	go func() {
		defer close(initializeDone)
		a.initialize(lifecycleCtx, basePath)
	}()
}

func (a *App) beforeClose(context.Context) bool {
	a.beginShutdown()
	return false
}

func (a *App) beginShutdown() {
	a.mu.Lock()
	if a.isShuttingDown {
		a.mu.Unlock()
		return
	}
	a.isShuttingDown = true
	operationCancel := a.cancel
	lifecycleCancel := a.serviceSet.cancel
	a.mu.Unlock()
	a.log("Shutdown started")
	if operationCancel != nil {
		operationCancel()
	}
	if lifecycleCancel != nil {
		lifecycleCancel()
	}
}

func (a *App) shutdown(context.Context) {
	a.beginShutdown()
	a.mu.Lock()
	gameDone := a.gameDone
	a.mu.Unlock()
	if gameDone != nil {
		err := appwindow.StopAll(gameProcessName)
		if err != nil {
			a.log("Game shutdown: " + err.Error())
		}
	}
	if gameDone != nil {
		a.waitForShutdown("Game", gameDone)
	}
	a.mu.Lock()
	clientFailuresDone := a.clientFailuresDone
	a.mu.Unlock()
	if clientFailuresDone != nil {
		a.waitForShutdown("Client failure watcher", clientFailuresDone)
	}
	a.stopServices()
	a.log("Shutdown finished")
}

func (a *App) GetStatus() LauncherStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return visibleLauncherStatus(a.status)
}

func (a *App) SetIdentity(identity string) LauncherStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	identity = strings.TrimSpace(identity)
	if a.status.Identity != identity {
		a.token = ""
		a.status.IsAuthenticated = false
		a.status.IdentityError = ""
	}
	a.status.Identity = identity
	a.updatePlayReadyLocked()
	a.emitStatusLocked()
	return visibleLauncherStatus(a.status)
}

func (a *App) SetSkipCinematic(isSkipped bool) {
	a.mu.Lock()
	a.arguments = setBooleanLaunchArgument(a.arguments, "skip-cinematic", isSkipped)
	a.status.IsCinematicSkipped = isSkipped
	a.mu.Unlock()
}

// Authorize obtains the short-lived desktop credential used by fang.dll.
// A production auth service may place its OAuth/browser flow behind the same
// desktop start and exchange contract used by the local development broker.
func (a *App) Authorize(identity string) error {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return errors.New("identity is required")
	}
	a.mu.Lock()
	isAuthOnline := a.status.IsAuthOnline
	lifecycleCtx := a.lifecycleCtx
	authURL := a.authURL
	a.mu.Unlock()
	if !isAuthOnline {
		return errors.New("authentication service is not online")
	}
	if isLoopbackAuthURL(authURL) {
		err := a.ensureLocalProfile(identity)
		if err != nil {
			criticalErr := fmt.Errorf("profileEnsure: %w", err)
			a.setCriticalFailure("Identity", criticalErr)
			return criticalErr
		}
	}
	a.setState("authorizing", "Signing in", 0)
	token, err := requestLaunchJWTWithBrowserContext(lifecycleCtx, authURL, identity, func(address string) {
		if a.ctx != nil {
			runtime.BrowserOpenURL(a.ctx, address)
		}
	})
	if err != nil {
		a.setCriticalFailure("Identity", fmt.Errorf("oauthAuthorize: %w", err))
		a.setState("error", fmt.Sprintf("Sign in failed: %v", err), 0)
		return fmt.Errorf("oauthAuthorize: %w", err)
	}
	a.mu.Lock()
	a.token = token
	a.status.Identity = identity
	a.status.IsAuthenticated = true
	a.status.IdentityError = ""
	a.status.State = "ready"
	a.status.Message = "Signed in"
	a.updatePlayReadyLocked()
	a.emitStatusLocked()
	a.mu.Unlock()
	a.log("OAuth credential ready for " + identity)
	return nil
}

func (a *App) SignOut() {
	a.mu.Lock()
	a.token = ""
	a.status.IsAuthenticated = false
	a.status.IdentityError = ""
	a.status.State = "idle"
	a.status.Message = "Signed out"
	a.updatePlayReadyLocked()
	a.emitStatusLocked()
	a.mu.Unlock()
}

func (a *App) Check() error {
	return a.startWork("checking", func(ctx context.Context) error {
		err := a.verifyGameIntegrity(ctx, a.gameDirectory(), "checking")
		if err != nil {
			criticalErr := fmt.Errorf("integrityCheck: %w", err)
			a.setCriticalFailure("Patch", criticalErr)
			return criticalErr
		}
		err = recordGameIntegrityVerification(filepath.Join(a.gameDirectory(), "darkspin.toml"), time.Now())
		if err != nil {
			return fmt.Errorf("integrityRecord: %w", err)
		}
		plan, err := buildModPatchPlan(ctx, a.gameDirectory())
		if err != nil {
			return fmt.Errorf("patchPlan: %w", err)
		}
		if plan != nil {
			a.setPlan(plan, "Patch check complete")
			return nil
		}
		a.setState("ready", "Game-file integrity verified", 100)
		return nil
	})
}

func (a *App) Patch() error {
	return a.startWork("checking", a.runPatch)
}

func (a *App) runPatch(ctx context.Context) (err error) {
	ctx = a.beginPatchOperation(ctx)
	defer func() {
		a.finishPatchOperation(err)
	}()
	err = a.verifyGameIntegrity(ctx, a.gameDirectory(), "checking")
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return fmt.Errorf("integrityCancel: %w", err)
		}
		criticalErr := fmt.Errorf("integrityCheck: %w", err)
		a.setCriticalFailure("Patch", criticalErr)
		return criticalErr
	}
	err = recordGameIntegrityVerification(filepath.Join(a.gameDirectory(), "darkspin.toml"), time.Now())
	if err != nil {
		return fmt.Errorf("integrityRecord: %w", err)
	}
	plan, err := buildModPatchPlan(ctx, a.gameDirectory())
	if err != nil {
		return fmt.Errorf("patchPlan: %w", err)
	}
	if plan != nil {
		err = a.applyModPatch(ctx, plan)
		if err != nil {
			return fmt.Errorf("patchApply: %w", err)
		}
		a.setPlan(plan, "Ready to play")
		return nil
	}
	a.setSubsystem("Patch", "Integrity verified", true)
	a.setState("ready", "Game-file integrity verified; installed files were not modified", 100)
	return nil
}

func buildModPatchPlan(ctx context.Context, root string) (*patcher.Plan, error) {
	if strings.TrimSpace(patchManifestURL) == "" {
		return nil, nil
	}
	plan, err := patcher.Build(ctx, patcher.Options{
		ManifestURL: patchManifestURL,
		Root:        filepath.Join(root, "mods"),
	})
	if err != nil {
		return nil, fmt.Errorf("planBuild: %w", err)
	}
	return plan, nil
}

func (a *App) applyModPatch(ctx context.Context, plan *patcher.Plan) error {
	err := patcher.Apply(ctx, plan, patcher.DispatchFunc(func(event patcher.Event) {
		a.mu.Lock()
		a.status.State = "patching"
		a.status.Message = event.Message
		a.status.Progress = event.Progress
		a.status.DownloadByte = event.CompletedByte
		a.emitStatusLocked()
		a.mu.Unlock()
	}))
	if err != nil {
		return fmt.Errorf("planApply: %w", err)
	}
	return nil
}

func (a *App) Play() error {
	a.mu.Lock()
	if a.isShuttingDown {
		a.mu.Unlock()
		return errors.New("DarkSpinner is shutting down")
	}
	if a.cancel != nil {
		a.mu.Unlock()
		return errors.New("another launcher operation is active")
	}
	if !a.status.IsPlayReady {
		a.mu.Unlock()
		return errors.New("identity, auth service, server, patch, and game must all be ready")
	}
	account := a.status.Identity
	isProfileRunning, err := isClientProfileRunning(a.status.GameDirectory, account)
	if err != nil {
		a.mu.Unlock()
		return fmt.Errorf("profileProcess: %w", err)
	}
	if isProfileRunning {
		a.mu.Unlock()
		return fmt.Errorf("Crogenitor %s already has a managed game client running", account)
	}
	arguments := append([]string(nil), a.arguments...)
	serverAddress, err := localServerAddress(a.serviceSet.gameServer)
	if err != nil {
		a.mu.Unlock()
		return fmt.Errorf("serverAddress: %w", err)
	}
	gameCtx, gameDone, gameCancel := a.beginGameLaunchLocked()
	a.status.State = "launching"
	a.status.Message = "Launching"
	a.updatePlayReadyLocked()
	a.emitStatusLocked()
	a.mu.Unlock()

	go a.runGameLaunch(gameCtx, gameLaunchRequest{
		account: account, serverAddress: serverAddress, arguments: arguments,
		done: gameDone, cancel: gameCancel,
	})
	return nil
}

type gameLaunchRequest struct {
	account       string
	clientProfile string
	serverAddress string
	token         string
	arguments     []string
	done          chan struct{}
	cancel        context.CancelFunc
}

func (a *App) runGameLaunch(ctx context.Context, req gameLaunchRequest) {
	defer req.cancel()
	defer a.finishGameLaunch(req.done)
	defer a.clearLaunchCredential(req.done)
	configPath := filepath.Join(a.gameDirectory(), "darkspin.toml")
	isDue, err := isGameIntegrityVerificationDue(configPath, time.Now())
	if err != nil {
		a.setCriticalFailure("Patch", fmt.Errorf("launchSchedule: %w", err))
		a.finish("error", fmt.Sprintf("Launch blocked: %v", err))
		return
	}
	if isDue {
		a.setState("launching", "Verifying pristine game files before launch", 0)
		err = a.verifyGameIntegrity(ctx, a.gameDirectory(), "launching")
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			a.setCriticalFailure("Patch", fmt.Errorf("launchIntegrity: %w", err))
			a.finish("error", fmt.Sprintf("Launch blocked: %v", err))
			return
		}
		err = recordGameIntegrityVerification(configPath, time.Now())
		if err != nil {
			a.setCriticalFailure("Patch", fmt.Errorf("launchRecord: %w", err))
			a.finish("error", fmt.Sprintf("Launch blocked: %v", err))
			return
		}
	}
	a.setState("launching", "Launching", 100)
	token := req.token
	if token == "" {
		var isAuthorized bool
		token, isAuthorized = a.authorizeGameLaunch(ctx, req.account)
		if !isAuthorized {
			return
		}
	}
	arguments := gameLaunchArguments(req.arguments, token)
	if req.serverAddress != "" {
		arguments = append(arguments, "--server-address", req.serverAddress)
	}
	launchID, resultPath, err := newClientFailureTarget(a.gameDirectory())
	if err != nil {
		a.finishGameLaunchFailure(fmt.Errorf("failureTarget: %w", err))
		return
	}
	clientProfile := req.clientProfile
	if clientProfile == "" {
		clientProfile = req.account
	}
	arguments = append([]string{
		"--client-profile", clientProfile, "--launch-id", launchID, "--client-result", resultPath,
	}, arguments...)
	a.log("Starting game")
	launchDone := make(chan struct{})
	detectionDone := make(chan struct{})
	go a.detectGameLaunch(ctx, launchDone, detectionDone, a.gameDirectory(), launchID)
	err = run(ctx, arguments)
	close(launchDone)
	<-detectionDone
	if !a.isAttachedGameLaunch(req.done) {
		if err != nil && !errors.Is(err, context.Canceled) {
			a.log(fmt.Sprintf("Detached game closed with an error: %v", err))
		}
		return
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		a.finishGameLaunchFailure(fmt.Errorf("gameLaunch: %w", err))
		return
	}
	a.finish("ready", "Game closed")
}

func (a *App) beginGameLaunchLocked() (context.Context, chan struct{}, context.CancelFunc) {
	lifecycleCtx := a.lifecycleCtx
	if lifecycleCtx == nil {
		lifecycleCtx = context.TODO()
	}
	gameCtx, gameCancel := context.WithCancel(context.WithoutCancel(lifecycleCtx))
	gameDone := make(chan struct{})
	a.gameDone = gameDone
	a.gameCancel = gameCancel
	go func() {
		select {
		case <-lifecycleCtx.Done():
			gameCancel()
		case <-gameDone:
		}
	}()
	return gameCtx, gameDone, gameCancel
}

// CloseRunningGame terminates the active client supervised by this launcher.
func (a *App) CloseRunningGame() error {
	a.mu.Lock()
	if a.gameDone == nil || a.gameCancel == nil || a.status.State != "running" {
		a.mu.Unlock()
		return errors.New("no active supervised game client is available to close")
	}
	gameDone := a.gameDone
	gameCancel := a.gameCancel
	a.status.State = "stopping"
	a.status.Message = "Closing game client"
	a.updatePlayReadyLocked()
	a.emitStatusLocked()
	a.mu.Unlock()
	gameCancel()
	select {
	case <-gameDone:
	case <-time.After(5 * time.Second):
		return errors.New("game client did not close within five seconds")
	}
	a.mu.Lock()
	a.token = ""
	a.status.IsAuthenticated = false
	a.mu.Unlock()
	a.finish("ready", "Game client closed")
	a.log("Active game client closed by launcher")
	return nil
}

func (a *App) isAttachedGameLaunch(done <-chan struct{}) bool {
	a.mu.Lock()
	isAttached := a.gameDone == done
	a.mu.Unlock()
	return isAttached
}

func (a *App) finishGameLaunchFailure(err error) {
	message := fmt.Sprintf("Launch failed: %v", err)
	a.log(message)
	a.mu.Lock()
	a.status.Game = "Ready to retry"
	a.status.GameError = ""
	a.status.IsGameReady = true
	a.status.LastRun = message
	a.status.IsLastRunFailure = true
	a.mu.Unlock()
	a.finish("error", message)
}

func (a *App) authorizeGameLaunch(ctx context.Context, account string) (string, bool) {
	if account == "" {
		return "", true
	}
	a.mu.Lock()
	authURL := a.authURL
	a.mu.Unlock()
	if isLoopbackAuthURL(authURL) {
		err := a.ensureLocalProfile(account)
		if err != nil {
			a.setCriticalFailure("Identity", fmt.Errorf("launchProfile: %w", err))
			a.finish("error", fmt.Sprintf("Crogenitor activation failed: %v", err))
			return "", false
		}
	}
	token, err := requestLaunchJWTContext(ctx, authURL, account)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return "", false
		}
		a.setCriticalFailure("Identity", fmt.Errorf("launchAuthorize: %w", err))
		a.finish("error", fmt.Sprintf("Sign in failed: %v", err))
		return "", false
	}
	a.mu.Lock()
	a.token = token
	a.status.IsAuthenticated = true
	a.emitStatusLocked()
	a.mu.Unlock()
	return token, true
}

func (a *App) finishGameLaunch(done chan struct{}) {
	close(done)
	a.mu.Lock()
	if a.gameDone == done {
		a.gameDone = nil
		a.gameCancel = nil
	}
	a.mu.Unlock()
}

func (a *App) detectGameLaunch(
	ctx context.Context, launchDone <-chan struct{}, detectionDone chan<- struct{},
	basePath, launchID string,
) {
	defer close(detectionDone)
	a.detectPlayingWithProbe(ctx, launchDone, func(string) (bool, error) {
		return isClientLaunchRunning(basePath, launchID), nil
	}, 100*time.Millisecond)
}

func (a *App) detectPlaying(ctx context.Context, launchDone <-chan struct{}) {
	a.detectPlayingWithProbe(ctx, launchDone, appwindow.IsRunning, 100*time.Millisecond)
}

func (a *App) detectPlayingWithProbe(
	ctx context.Context, launchDone <-chan struct{},
	isRunning func(string) (bool, error), interval time.Duration,
) {
	if ctx == nil || launchDone == nil || isRunning == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		isDetected, err := isRunning(gameProcessName)
		if err == nil && isDetected {
			a.mu.Lock()
			if a.status.State == "launching" {
				a.status.State = "running"
				a.status.Message = "Playing"
				a.updatePlayReadyLocked()
				a.emitStatusLocked()
			}
			a.mu.Unlock()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-launchDone:
			return
		case <-ticker.C:
		}
	}
}

// IsProfileRunning reports whether a managed client owns identity.
func (a *App) IsProfileRunning(identity string) (bool, error) {
	identity = strings.TrimSpace(identity)
	err := validateProfileIdentity(identity)
	if err != nil {
		return false, fmt.Errorf("profileValidate: %w", err)
	}
	records, err := a.runningProfileRecords(identity)
	if err != nil {
		return false, fmt.Errorf("profileProcess: %w", err)
	}
	return len(records) != 0, nil
}

// CloseRunningProfile stops only managed clients that own identity.
func (a *App) CloseRunningProfile(identity string) error {
	identity = strings.TrimSpace(identity)
	err := validateProfileIdentity(identity)
	if err != nil {
		return fmt.Errorf("profileValidate: %w", err)
	}
	records, err := a.runningProfileRecords(identity)
	if err != nil {
		return fmt.Errorf("profileProcesses: %w", err)
	}
	for _, record := range records {
		err = appwindow.StopProcess(record.PID)
		if err != nil {
			return fmt.Errorf("profileStop[%d]: %w", record.PID, err)
		}
	}
	return nil
}

// HasDetachedGameInstances reports whether any Game process is not owned
// by a live attached launcher session.
func (a *App) HasDetachedGameInstances() (bool, error) {
	processIDs, err := a.detachedGameProcessIDs()
	if err != nil {
		return false, fmt.Errorf("detachedProcesses: %w", err)
	}
	return len(processIDs) != 0, nil
}

// CloseDetachedGameInstances preserves attached launcher sessions and closes
// every detached or otherwise untracked Game process.
func (a *App) CloseDetachedGameInstances() error {
	processIDs, err := a.detachedGameProcessIDs()
	if err != nil {
		return fmt.Errorf("detachedProcesses: %w", err)
	}
	closedCount := 0
	failedCount := 0
	for _, processID := range processIDs {
		err = appwindow.StopProcess(processID)
		if err != nil {
			a.log(fmt.Sprintf(
				"Detached game cleanup failed for PID %d: %v", processID, err,
			))
			failedCount++
			continue
		}
		closedCount++
	}
	if closedCount != 0 {
		a.log(fmt.Sprintf("Closed %d detached or untracked game clients", closedCount))
	}
	if failedCount == 1 {
		return errors.New("one game client could not be closed; see the launcher log")
	}
	if failedCount != 0 {
		return fmt.Errorf(
			"%d game clients could not be closed; see the launcher log", failedCount,
		)
	}
	return nil
}

func (a *App) detachedGameProcessIDs() ([]uint32, error) {
	processIDs, err := appwindow.Running(gameProcessName)
	if err != nil {
		return nil, fmt.Errorf("gameProcesses: %w", err)
	}
	attachedRecords, err := runningAttachedClientRecords(a.gameDirectory())
	if err != nil {
		return nil, fmt.Errorf("attachedRecords: %w", err)
	}
	attachedProcessIDs := make(map[uint32]struct{}, len(attachedRecords))
	for _, record := range attachedRecords {
		attachedProcessIDs[record.PID] = struct{}{}
	}
	detachedProcessIDs := make([]uint32, 0, len(processIDs))
	for _, processID := range processIDs {
		if _, isAttached := attachedProcessIDs[processID]; isAttached {
			continue
		}
		detachedProcessIDs = append(detachedProcessIDs, processID)
	}
	return detachedProcessIDs, nil
}

func (a *App) runningProfileRecords(identity string) ([]clientProcessRecord, error) {
	basePath := a.gameDirectory()
	records, err := runningClientProfileRecords(basePath, identity)
	if err != nil || len(records) != 0 {
		return records, err
	}
	a.mu.Lock()
	isLegacyAttached := a.status.State == "running" &&
		strings.EqualFold(strings.TrimSpace(a.status.Identity), strings.TrimSpace(identity))
	a.mu.Unlock()
	if !isLegacyAttached {
		return records, nil
	}
	legacyRecords, err := runningUnownedAttachedClientRecords(basePath)
	if err != nil {
		return nil, fmt.Errorf("legacyRecords: %w", err)
	}
	if len(legacyRecords) != 1 {
		return records, nil
	}
	return legacyRecords, nil
}

func (a *App) Cancel() {
	a.mu.Lock()
	cancel := a.cancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) startWork(state string, work func(context.Context) error) error {
	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return errors.New("another launcher operation is active")
	}
	baseContext := a.ctx
	if baseContext == nil {
		baseContext = context.TODO()
	}
	ctx, cancel := context.WithCancel(baseContext)
	a.cancel = cancel
	a.status.State = state
	a.status.Message = strings.ToUpper(state[:1]) + state[1:]
	a.emitStatusLocked()
	a.mu.Unlock()
	go a.runWork(ctx, work)
	return nil
}

func (a *App) runWork(ctx context.Context, work func(context.Context) error) {
	err := work(ctx)
	a.mu.Lock()
	a.cancel = nil
	if err == nil {
		a.updatePlayReadyLocked()
		a.emitStatusLocked()
	}
	a.mu.Unlock()
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) {
		a.finish("cancelled", "Cancelled")
		return
	}
	a.finish("error", err.Error())
}

func (a *App) setState(state, message string, progress int) {
	a.mu.Lock()
	a.status.State = state
	a.status.Message = message
	a.status.Progress = progress
	a.emitStatusLocked()
	a.mu.Unlock()
}

func (a *App) setPlan(plan *patcher.Plan, message string) {
	a.mu.Lock()
	a.status.State = "ready"
	a.status.Message = message
	a.status.Progress = 100
	a.status.RequiredFile = plan.DownloadCount()
	a.status.DeleteFile = 0
	a.status.DownloadByte = plan.DownloadByte()
	a.status.Patch = "Complete"
	a.status.PatchProgress = 100
	a.status.PatchError = ""
	a.status.IsPatchComplete = true
	a.updatePlayReadyLocked()
	a.emitStatusLocked()
	a.mu.Unlock()
}

func (a *App) finish(state, message string) {
	a.mu.Lock()
	a.status.State = state
	a.status.Message = message
	a.status.Progress = 100
	a.updatePlayReadyLocked()
	a.emitStatusLocked()
	a.mu.Unlock()
	if state != "error" {
		a.log(message)
	}
}

func (a *App) updatePlayReadyLocked() {
	isIdle := a.cancel == nil && a.status.State != "launching" && a.status.State != "running"
	a.status.IsPlayReady = isIdle && strings.TrimSpace(a.status.Identity) != "" &&
		a.status.IsAuthOnline && a.status.IsServerOnline && a.status.IsPatchComplete &&
		a.status.IsGameReady && a.status.IsProfileStoreReady && a.status.IsContentReady
}

func (a *App) clearLaunchCredential(done <-chan struct{}) {
	a.mu.Lock()
	if a.gameDone != done {
		a.mu.Unlock()
		return
	}
	a.token = ""
	a.status.IsAuthenticated = false
	a.updatePlayReadyLocked()
	a.emitStatusLocked()
	a.mu.Unlock()
}

func (a *App) gameDirectory() string {
	a.mu.Lock()
	directory := a.status.GameDirectory
	a.mu.Unlock()
	return directory
}

func (a *App) emitStatusLocked() {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "darkspinner:status", visibleLauncherStatus(a.status))
	}
}

func (a *App) log(message string) {
	a.mu.Lock()
	gameDirectory := a.status.GameDirectory
	a.mu.Unlock()
	if strings.TrimSpace(gameDirectory) == "" {
		return
	}
	logPath := filepath.Join(gameDirectory, "darkspin", "logs", "darkspinner.log")
	a.logMu.Lock()
	output, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		_, _ = fmt.Fprintf(output, "%s %s\n", time.Now().Format(time.RFC3339Nano), visibleLauncherText(message))
		_ = output.Close()
	}
	a.logMu.Unlock()
}

func visibleLauncherStatus(status LauncherStatus) LauncherStatus {
	status.Message = visibleLauncherText(status.Message)
	status.IdentityError = visibleLauncherText(status.IdentityError)
	status.AuthError = visibleLauncherText(status.AuthError)
	status.ServerError = visibleLauncherText(status.ServerError)
	status.PatchError = visibleLauncherText(status.PatchError)
	status.GameError = visibleLauncherText(status.GameError)
	status.AvatarError = visibleLauncherText(status.AvatarError)
	status.ProfileError = visibleLauncherText(status.ProfileError)
	status.ContentError = visibleLauncherText(status.ContentError)
	status.LastRun = visibleLauncherText(status.LastRun)
	status.LauncherNotice = visibleLauncherText(status.LauncherNotice)
	status.GameDirectory = visibleLauncherText(status.GameDirectory)
	return status
}

func visibleLauncherText(message string) string {
	return visibleProfilePattern.ReplaceAllStringFunc(message, func(term string) string {
		if strings.EqualFold(term, "profiles") {
			return "Crogenitors"
		}
		return "Crogenitor"
	})
}

func launchArgumentValue(arguments []string, name string) string {
	longName := "--" + name
	for index := 0; index < len(arguments); index++ {
		if strings.HasPrefix(arguments[index], longName+"=") {
			return strings.TrimSpace(strings.TrimPrefix(arguments[index], longName+"="))
		}
		if arguments[index] == longName && index+1 < len(arguments) {
			return strings.TrimSpace(arguments[index+1])
		}
	}
	return ""
}

func hasLaunchArgument(arguments []string, name string) bool {
	longName := "--" + name
	for _, argument := range arguments {
		if argument == longName || argument == "-"+name || argument == longName+"=true" {
			return true
		}
	}
	return false
}

func gameLaunchArguments(arguments []string, token string) []string {
	result := make([]string, 0, len(arguments)+2)
	isClientTraceConfigured := false
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "--auto-play" || argument == "-auto-play" {
			continue
		}
		if argument == "--account" || argument == "-account" || argument == "--jwt" || argument == "-jwt" {
			index++
			continue
		}
		if strings.HasPrefix(argument, "--account=") || strings.HasPrefix(argument, "--jwt=") {
			continue
		}
		if argument == "--client-trace" || argument == "-client-trace" ||
			argument == "--trace" || argument == "-trace" ||
			strings.HasPrefix(argument, "--client-trace=") || strings.HasPrefix(argument, "--trace=") {
			isClientTraceConfigured = true
		}
		result = append(result, argument)
	}
	if !isClientTraceConfigured {
		result = append(result, "--client-trace", "game.jsonl")
	}
	if token != "" {
		result = append(result, "--jwt", token)
	}
	return result
}

func setBooleanLaunchArgument(arguments []string, name string, isEnabled bool) []string {
	longName := "--" + name
	shortName := "-" + name
	result := make([]string, 0, len(arguments)+1)
	if isEnabled {
		// Launcher flags must precede positional client arguments and "--".
		result = append(result, longName)
	}
	for _, argument := range arguments {
		if argument == longName || argument == shortName ||
			strings.HasPrefix(argument, longName+"=") || strings.HasPrefix(argument, shortName+"=") {
			continue
		}
		result = append(result, argument)
	}
	return result
}

func executableDirectory() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("executablePath: %w", err)
	}
	directory := filepath.Dir(executable)
	if goruntime.GOOS == "darwin" && filepath.Base(directory) == "MacOS" && filepath.Base(filepath.Dir(directory)) == "Contents" {
		bundlePath := filepath.Dir(filepath.Dir(directory))
		if strings.HasSuffix(bundlePath, ".app") {
			return filepath.Dir(bundlePath), nil
		}
	}
	return directory, nil
}
