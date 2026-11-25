package main

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
)

var errElevatedLaunch = errors.New("Darkspinner cannot run as an administrator")

const elevatedLaunchKey = "is_elevated_launch_allowed"

func (e *App) configureElevatedLaunch() {
	if runtime.GOOS != "windows" {
		return
	}
	isAllowed, err := readElevatedLaunchPreference()
	if err != nil {
		writeStartupDiagnostic("DarkSpinner administrator launch preference", err)
	}
	e.status.IsElevatedLaunchSupported = true
	e.status.IsElevatedLaunchAllowed = isAllowed
	e.status.IsStartupBlocked = ensureStandardUser() != nil && !isAllowed
}

func readElevatedLaunchPreference() (bool, error) {
	basePath, err := executableDirectory()
	if err != nil {
		return false, fmt.Errorf("elevationDirectory: %w", err)
	}
	paths, err := resolveSpinnerBasePaths(basePath)
	if err != nil {
		return false, fmt.Errorf("elevationPaths: %w", err)
	}
	settings, err := readLauncherSettings(paths.configPath)
	if err != nil {
		return false, fmt.Errorf("elevationRead: %w", err)
	}
	return settings.IsElevatedLaunchAllowed, nil
}

// SetElevatedLaunchAllowed saves the player's choice and resumes a launcher
// waiting for acknowledgement once that choice is durable.
func (e *App) SetElevatedLaunchAllowed(isAllowed bool) error {
	if runtime.GOOS != "windows" {
		return errors.New("administrator launch preference is only available on Windows")
	}
	paths, err := e.spinnerPaths()
	if err != nil {
		return fmt.Errorf("elevationConfigPath: %w", err)
	}
	e.mu.Lock()
	if e.ctx == nil || e.isShuttingDown {
		e.mu.Unlock()
		return errors.New("launcher is unavailable")
	}
	if e.status.IsStartupBlocked && !isAllowed {
		e.mu.Unlock()
		return errors.New("launcher startup is waiting for administrator acknowledgement")
	}
	err = writeLauncherConfigLine(paths.configPath, elevatedLaunchKey, elevatedLaunchKey+" = "+strconv.FormatBool(isAllowed))
	if err != nil {
		e.mu.Unlock()
		writeStartupDiagnostic("DarkSpinner administrator launch preference", err)
		return fmt.Errorf("elevationSave: %w", err)
	}
	isResuming := isAllowed && e.status.IsStartupBlocked && !e.status.IsElevatedLaunchAllowed
	ctx := e.ctx
	e.status.IsElevatedLaunchAllowed = isAllowed
	e.emitStatusLocked()
	e.mu.Unlock()
	if isResuming {
		e.startLauncher(ctx)
		e.mu.Lock()
		e.status.IsStartupBlocked = false
		e.emitStatusLocked()
		e.mu.Unlock()
	}
	return nil
}
