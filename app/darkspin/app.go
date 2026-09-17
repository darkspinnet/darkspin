//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/patcher"
	appwindow "github.com/darkspinnet/darkspin/window"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const gameProcessName = "Darkspore.exe"

type App struct {
	ctx       context.Context
	mu        sync.Mutex
	cancel    context.CancelFunc
	arguments []string
	token     string
	status    LauncherStatus
}

type LauncherStatus struct {
	State               string `json:"state"`
	Message             string `json:"message"`
	Account             string `json:"account"`
	ManifestURL         string `json:"manifestUrl"`
	GameDirectory       string `json:"gameDirectory"`
	Version             string `json:"version"`
	Progress            int    `json:"progress"`
	RequiredFile        int    `json:"requiredFile"`
	DeleteFile          int    `json:"deleteFile"`
	DownloadByte        int64  `json:"downloadByte"`
	IsAuthenticated     bool   `json:"isAuthenticated"`
	IsAutoPlayRequested bool   `json:"isAutoPlayRequested"`
	IsPatchEnabled      bool   `json:"isPatchEnabled"`
	IsCinematicSkipped  bool   `json:"isCinematicSkipped"`
}

func NewApp(arguments []string) *App {
	account := launchArgumentValue(arguments, "account")
	return &App{
		arguments: setBooleanLaunchArgument(arguments, "skip-cinematic", false),
		status: LauncherStatus{
			State:               "idle",
			Message:             "Ready",
			Account:             account,
			ManifestURL:         strings.TrimSpace(patchManifestURL),
			Version:             Version,
			IsAutoPlayRequested: hasLaunchArgument(arguments, "auto-play"),
			IsPatchEnabled:      strings.TrimSpace(patchManifestURL) != "",
			IsCinematicSkipped:  false,
		},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	executable, err := executableDirectory()
	if err == nil {
		a.mu.Lock()
		a.status.GameDirectory = executable
		a.mu.Unlock()
	}
}

func (a *App) shutdown(context.Context) {
	a.Cancel()
}

func (a *App) GetStatus() LauncherStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

func (a *App) SetAccount(account string) LauncherStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	account = strings.TrimSpace(account)
	if a.status.Account != account {
		a.token = ""
		a.status.IsAuthenticated = false
	}
	a.status.Account = account
	return a.status
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
func (a *App) Authorize(account string) error {
	account = strings.TrimSpace(account)
	if account == "" {
		return errors.New("account is required")
	}
	a.setState("authorizing", "Signing in", 0)
	token, err := requestLaunchJWTWithBrowser(authServiceURL, account, func(address string) {
		if a.ctx != nil {
			runtime.BrowserOpenURL(a.ctx, address)
		}
	})
	if err != nil {
		a.setState("error", fmt.Sprintf("Sign in failed: %v", err), 0)
		return fmt.Errorf("oauthAuthorize: %w", err)
	}
	a.mu.Lock()
	a.token = token
	a.status.Account = account
	a.status.IsAuthenticated = true
	a.status.State = "ready"
	a.status.Message = "Signed in"
	a.emitStatusLocked()
	a.mu.Unlock()
	a.log("OAuth credential ready for " + account)
	return nil
}

func (a *App) SignOut() {
	a.mu.Lock()
	a.token = ""
	a.status.IsAuthenticated = false
	a.status.State = "idle"
	a.status.Message = "Signed out"
	a.emitStatusLocked()
	a.mu.Unlock()
}

func (a *App) Check() error {
	if strings.TrimSpace(patchManifestURL) == "" {
		a.setState("ready", "Patching is not configured for this build", 100)
		return nil
	}
	return a.startWork("checking", func(ctx context.Context) error {
		plan, err := patcher.Build(ctx, patcher.Options{
			ManifestURL: patchManifestURL,
			Root:        filepath.Join(a.gameDirectory(), "mods"),
		})
		if err != nil {
			return fmt.Errorf("patchCheck: %w", err)
		}
		a.setPlan(plan, "Patch check complete")
		return nil
	})
}

func (a *App) Patch() error {
	if strings.TrimSpace(patchManifestURL) == "" {
		a.setState("ready", "Patching is not configured for this build", 100)
		return nil
	}
	return a.startWork("patching", func(ctx context.Context) error {
		plan, err := patcher.Build(ctx, patcher.Options{
			ManifestURL: patchManifestURL,
			Root:        filepath.Join(a.gameDirectory(), "mods"),
		})
		if err != nil {
			return fmt.Errorf("patchPlan: %w", err)
		}
		err = patcher.Apply(ctx, plan, patcher.DispatchFunc(func(event patcher.Event) {
			a.setState("patching", event.Message, event.Progress)
			a.log(event.Message)
		}))
		if err != nil {
			return fmt.Errorf("patchApply: %w", err)
		}
		a.setPlan(plan, "Ready to play")
		return nil
	})
}

func (a *App) Play() error {
	processIDs, err := appwindow.Running(gameProcessName)
	if err != nil {
		return fmt.Errorf("gameProcess: %w", err)
	}
	baselineProcessCount := len(processIDs)
	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return errors.New("another launcher operation is active")
	}
	account := a.status.Account
	arguments := append([]string(nil), a.arguments...)
	a.status.State = "launching"
	a.status.Message = "Starting game"
	a.emitStatusLocked()
	a.mu.Unlock()

	go a.runGameLaunch(gameLaunchRequest{
		account: account, arguments: arguments,
		baselineProcessCount: baselineProcessCount,
	})
	return nil
}

type gameLaunchRequest struct {
	account              string
	arguments            []string
	baselineProcessCount int
}

func (a *App) runGameLaunch(req gameLaunchRequest) {
	token := ""
	if req.account != "" {
		issuedToken, err := requestLaunchJWT(authServiceURL, req.account)
		if err != nil {
			a.finish("error", fmt.Sprintf("Sign in failed: %v", err))
			return
		}
		token = issuedToken
		a.mu.Lock()
		a.token = token
		a.status.IsAuthenticated = true
		a.emitStatusLocked()
		a.mu.Unlock()
	}
	arguments := gameLaunchArguments(req.arguments, token)
	a.log("Starting game")
	launchDone := make(chan struct{})
	detectionDone := make(chan struct{})
	go a.detectGameLaunch(
		launchDone, detectionDone, req.baselineProcessCount,
	)
	err := run(arguments)
	close(launchDone)
	<-detectionDone
	if err != nil {
		a.finish("error", fmt.Sprintf("Launch failed: %v", err))
		return
	}
	isRunning, processErr := appwindow.IsRunning(gameProcessName)
	if processErr == nil && isRunning {
		a.finish("running", "Playing")
		return
	}
	a.finish("ready", "Game closed")
}

func (a *App) detectGameLaunch(
	launchDone <-chan struct{}, detectionDone chan<- struct{},
	baselineProcessCount int,
) {
	defer close(detectionDone)
	a.detectAdditionalGame(launchDone, baselineProcessCount)
}

func (a *App) detectAdditionalGame(launchDone <-chan struct{}, baselineProcessCount int) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		processIDs, err := appwindow.Running(gameProcessName)
		if err == nil && len(processIDs) > baselineProcessCount {
			a.setState("running", "Playing", 100)
			return
		}
		select {
		case <-launchDone:
			return
		case <-ticker.C:
		}
	}
}

// IsGameRunning reports whether another Game client is active.
func (a *App) IsGameRunning() (bool, error) {
	isRunning, err := appwindow.IsRunning(gameProcessName)
	if err != nil {
		return false, fmt.Errorf("gameProcess: %w", err)
	}
	return isRunning, nil
}

// CloseRunningGame stops active Game clients after UI confirmation.
func (a *App) CloseRunningGame() error {
	err := appwindow.StopAll(gameProcessName)
	if err != nil {
		return fmt.Errorf("gameStop: %w", err)
	}
	return nil
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
	a.emitStatusLocked()
	a.mu.Unlock()
}

func (a *App) finish(state, message string) {
	a.setState(state, message, 100)
	a.log(message)
}

func (a *App) gameDirectory() string {
	a.mu.Lock()
	directory := a.status.GameDirectory
	a.mu.Unlock()
	return directory
}

func (a *App) emitStatusLocked() {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "darkspin:status", a.status)
	}
}

func (a *App) log(message string) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "darkspin:log", message)
	}
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
	for _, value := range arguments {
		if value == longName || value == "-"+name || value == longName+"=true" {
			return true
		}
	}
	return false
}

func gameLaunchArguments(arguments []string, token string) []string {
	result := make([]string, 0, len(arguments)+2)
	for index := 0; index < len(arguments); index++ {
		value := arguments[index]
		if value == "--auto-play" || value == "-auto-play" {
			continue
		}
		if value == "--account" || value == "-account" || value == "--jwt" || value == "-jwt" {
			index++
			continue
		}
		if strings.HasPrefix(value, "--account=") || strings.HasPrefix(value, "--jwt=") {
			continue
		}
		result = append(result, value)
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
	for _, value := range arguments {
		if value == longName || value == shortName ||
			strings.HasPrefix(value, longName+"=") || strings.HasPrefix(value, shortName+"=") {
			continue
		}
		result = append(result, value)
	}
	return result
}

func executableDirectory() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("executablePath: %w", err)
	}
	return filepath.Dir(executable), nil
}
