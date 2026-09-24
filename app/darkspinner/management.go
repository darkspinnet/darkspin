package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	appwindow "github.com/darkspinnet/darkspin/window"
)

const (
	launcherIntegrationStartMenu = "start-menu"
	launcherIntegrationDesktop   = "desktop"
	launcherIntegrationSteam     = "steam"
)

// LauncherIntegrationStatus reports the optional integrations owned by
// Darkspinner without exposing platform-specific paths to the frontend.
type LauncherIntegrationStatus struct {
	IsStartMenuInstalled   bool   `json:"isStartMenuInstalled"`
	IsDesktopInstalled     bool   `json:"isDesktopInstalled"`
	IsSteamLaunchInstalled bool   `json:"isSteamLaunchInstalled"`
	IsManagementSupported  bool   `json:"isManagementSupported"`
	Message                string `json:"message"`
}

func launcherManagementPaths() (string, string, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("managementExecutable: %w", err)
	}
	executablePath, err = filepath.Abs(executablePath)
	if err != nil {
		return "", "", fmt.Errorf("managementPath: %w", err)
	}
	basePath := filepath.Dir(executablePath)
	if !isLocalGameRoot(basePath) {
		return "", "", errors.New("Darkspinner must be beside the game before integrations can be managed")
	}
	return executablePath, basePath, nil
}

// GetLauncherIntegrationStatus inspects optional shortcuts and Steam launch
// integration owned by this launcher.
func (e *App) GetLauncherIntegrationStatus() LauncherIntegrationStatus {
	executablePath, basePath, err := launcherManagementPaths()
	if err != nil {
		return LauncherIntegrationStatus{Message: err.Error()}
	}
	status, err := inspectLauncherIntegrations(executablePath, basePath)
	if err != nil {
		status.Message = err.Error()
	}
	return status
}

// RepairLauncherIntegration creates or refreshes one optional integration.
func (e *App) RepairLauncherIntegration(kind string) (LauncherIntegrationStatus, error) {
	kind = strings.TrimSpace(kind)
	if !isLauncherIntegrationKind(kind) {
		return LauncherIntegrationStatus{}, errors.New("unknown launcher integration")
	}
	executablePath, basePath, err := launcherManagementPaths()
	if err != nil {
		return LauncherIntegrationStatus{}, fmt.Errorf("integrationPaths: %w", err)
	}
	err = repairLauncherIntegration(kind, executablePath, basePath)
	if err != nil {
		return LauncherIntegrationStatus{}, fmt.Errorf("integrationRepair: %w", err)
	}
	status, err := inspectLauncherIntegrations(executablePath, basePath)
	if err != nil {
		return status, fmt.Errorf("integrationInspect: %w", err)
	}
	e.log("Launcher integration repaired: " + kind)
	return status, nil
}

// RemoveLauncherIntegration removes one optional integration when it is owned
// by Darkspinner.
func (e *App) RemoveLauncherIntegration(kind string) (LauncherIntegrationStatus, error) {
	kind = strings.TrimSpace(kind)
	if !isLauncherIntegrationKind(kind) {
		return LauncherIntegrationStatus{}, errors.New("unknown launcher integration")
	}
	executablePath, basePath, err := launcherManagementPaths()
	if err != nil {
		return LauncherIntegrationStatus{}, fmt.Errorf("integrationPaths: %w", err)
	}
	err = removeLauncherIntegration(kind, executablePath, basePath)
	if err != nil {
		return LauncherIntegrationStatus{}, fmt.Errorf("integrationRemove: %w", err)
	}
	status, err := inspectLauncherIntegrations(executablePath, basePath)
	if err != nil {
		return status, fmt.Errorf("integrationInspect: %w", err)
	}
	e.log("Launcher integration removed: " + kind)
	return status, nil
}

// UninstallDarkspinner removes Darkspinner-owned integrations, runtime data,
// and this launcher after the process exits. Shipped game content is retained.
func (e *App) UninstallDarkspinner() error {
	isRunning, err := appwindow.IsRunning(gameProcessName)
	if err != nil {
		return fmt.Errorf("uninstallGameCheck: %w", err)
	}
	if isRunning {
		return errors.New("close every running game client before uninstalling Darkspinner")
	}
	executablePath, basePath, err := launcherManagementPaths()
	if err != nil {
		return fmt.Errorf("uninstallPaths: %w", err)
	}
	for _, kind := range []string{
		launcherIntegrationStartMenu,
		launcherIntegrationDesktop,
		launcherIntegrationSteam,
	} {
		err = removeLauncherIntegration(kind, executablePath, basePath)
		if err != nil {
			return fmt.Errorf("uninstallIntegration[%s]: %w", kind, err)
		}
	}
	err = scheduleLauncherUninstall(executablePath, basePath, os.Getpid())
	if err != nil {
		return fmt.Errorf("uninstallSchedule: %w", err)
	}
	e.beginShutdown()
	e.quit()
	return nil
}

func isLauncherIntegrationKind(kind string) bool {
	return kind == launcherIntegrationStartMenu ||
		kind == launcherIntegrationDesktop ||
		kind == launcherIntegrationSteam
}
