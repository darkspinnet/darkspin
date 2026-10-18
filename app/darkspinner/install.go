package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	launcherExecutableName = "darkspinner.exe"
	steamDemoAppID         = "102830"
)

// InstallationStatus describes the launcher placement and required Steam
// installation without exposing registry or filesystem implementation details
// to the frontend.
type InstallationStatus struct {
	IsLocalReady     bool   `json:"isLocalReady"`
	IsSteamInstalled bool   `json:"isSteamInstalled"`
	IsGameInstalled  bool   `json:"isGameInstalled"`
	CanRelocate      bool   `json:"canRelocate"`
	Message          string `json:"message"`
}

// RelocationRequest selects optional per-user launcher integrations to create
// after the relocated executable starts from the game root.
type RelocationRequest struct {
	IsStartMenuShortcut bool `json:"isStartMenuShortcut"`
	IsDesktopShortcut   bool `json:"isDesktopShortcut"`
	IsSteamLaunch       bool `json:"isSteamLaunch"`
}

func inspectInstallation(basePath string) (InstallationStatus, string) {
	if isLocalGameRoot(basePath) {
		return InstallationStatus{
			IsLocalReady: true, IsSteamInstalled: true, IsGameInstalled: true,
			Message: "Game files are ready.",
		}, basePath
	}
	steamRootSet := discoverSteamRoots()
	status := InstallationStatus{IsSteamInstalled: len(steamRootSet) > 0}
	gameRoot := discoverSteamGameRoot(steamRootSet)
	if gameRoot != "" {
		status.IsSteamInstalled = true
		status.IsGameInstalled = true
		status.CanRelocate = isAutomaticRelocationSupported()
		if status.CanRelocate {
			status.Message = "The required Steam demo is installed. DarkSpinner can move beside it and restart."
		} else {
			status.Message = "The required Steam demo is installed. Place DarkSpinner beside its game folders and restart it."
		}
		return status, gameRoot
	}
	if status.IsSteamInstalled {
		status.Message = "The required Steam demo was not found."
		return status, ""
	}
	status.Message = "Steam is required before DarkSpinner can continue."
	return status, ""
}

func isLocalGameRoot(basePath string) bool {
	binPath := filepath.Join(basePath, "DarksporeBin")
	fi, err := os.Stat(binPath)
	return err == nil && fi.IsDir()
}

// GetInstallationStatus returns the most recent installation scan.
func (a *App) GetInstallationStatus() InstallationStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.installation
}

// RefreshInstallationStatus scans Steam and the current launcher directory
// again after an installation finishes.
func (a *App) RefreshInstallationStatus() InstallationStatus {
	basePath, err := executableDirectory()
	if err != nil {
		return InstallationStatus{Message: "Unable to locate DarkSpinner."}
	}
	status, gameRoot := inspectInstallation(basePath)
	a.mu.Lock()
	a.installation = status
	a.discoveredGameRoot = gameRoot
	a.mu.Unlock()
	return status
}

// RestartLauncher exits this process and starts the same executable after the
// single-instance lock has been released.
func (a *App) RestartLauncher() error {
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("restartExecutable: %w", err)
	}
	err = restartAfterExit(executablePath, a.presentationArguments())
	if err != nil {
		return fmt.Errorf("restartStart: %w", err)
	}
	a.beginShutdown()
	a.quit()
	return nil
}

// OpenSteamDemoInstall asks Steam to install the required demo.
func (a *App) OpenSteamDemoInstall() error {
	a.mu.Lock()
	isSteamInstalled := a.installation.IsSteamInstalled
	a.mu.Unlock()
	if !isSteamInstalled {
		return errors.New("Steam is not installed")
	}
	err := a.openURL("steam://install/" + steamDemoAppID)
	if err != nil {
		return fmt.Errorf("steamOpen: %w", err)
	}
	return nil
}

// RelocateToGameRoot copies this executable beside the detected game folders,
// starts the relocated copy, and exits this instance.
func (a *App) RelocateToGameRoot(req RelocationRequest) error {
	a.mu.Lock()
	status := a.installation
	gameRoot := a.discoveredGameRoot
	a.mu.Unlock()
	if !status.CanRelocate || !status.IsGameInstalled || strings.TrimSpace(gameRoot) == "" {
		return errors.New("installed game files were not found")
	}
	if !isLocalGameRoot(gameRoot) {
		return errors.New("detected game installation is incomplete")
	}
	sourcePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("relocateSource: %w", err)
	}
	sourcePath, err = filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("relocateSourcePath: %w", err)
	}
	destinationPath := filepath.Join(gameRoot, launcherExecutableName)
	if strings.EqualFold(filepath.Clean(sourcePath), filepath.Clean(destinationPath)) {
		return errors.New("DarkSpinner is already beside the game")
	}
	restartArguments := relocationArguments(req)
	restartArguments = append(restartArguments, a.presentationArguments()...)
	err = relocateAndRestart(sourcePath, destinationPath, restartArguments)
	if err != nil {
		return fmt.Errorf("relocateStart: %w", err)
	}
	a.beginShutdown()
	a.quit()
	return nil
}

func (e *App) presentationArguments() []string {
	if e.isHeadless {
		return []string{"--headless"}
	}
	return nil
}

func relocationArguments(req RelocationRequest) []string {
	arguments := make([]string, 0, 3)
	if req.IsStartMenuShortcut {
		arguments = append(arguments, "--install-start-menu-shortcut")
	}
	if req.IsDesktopShortcut {
		arguments = append(arguments, "--install-desktop-shortcut")
	}
	if req.IsSteamLaunch {
		arguments = append(arguments, "--install-steam-launch")
	}
	return arguments
}

func removeInstallationArguments(arguments []string) []string {
	filteredArguments := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		switch argument {
		case "--install-start-menu-shortcut", "--install-desktop-shortcut", "--install-steam-launch", "--steam-launch":
			continue
		}
		filteredArguments = append(filteredArguments, argument)
	}
	return filteredArguments
}
