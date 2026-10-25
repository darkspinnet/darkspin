package main

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/darkspinnet/darkspin/server/game"
)

func (e *App) restoreMoviePreferences(basePath string) error {
	settings, err := readLauncherSettings(filepath.Join(basePath, game.DefaultConfigFilename))
	if err != nil {
		return fmt.Errorf("movieRead: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	// Explicit launch flags override saved preferences for this run only.
	isIntroSkipped := settings.IsIntroSkipped || hasLaunchArgument(e.arguments, "skip-intro")
	isCinematicSkipped := settings.IsCinematicSkipped || hasLaunchArgument(e.arguments, "skip-cinematic")
	e.arguments = setBooleanLaunchArgument(e.arguments, "skip-intro", isIntroSkipped)
	e.arguments = setBooleanLaunchArgument(e.arguments, "skip-cinematic", isCinematicSkipped)
	e.status.IsIntroSkipped = isIntroSkipped
	e.status.IsCinematicSkipped = isCinematicSkipped
	e.emitStatusLocked()
	return nil
}

// SetSkipIntro persists the choice before applying it to future launches.
func (e *App) SetSkipIntro(isSkipped bool) error {
	paths, err := e.spinnerPaths()
	if err != nil {
		return fmt.Errorf("introPath: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	err = writeLauncherConfigLine(paths.configPath, "is_intro_skipped", "is_intro_skipped = "+strconv.FormatBool(isSkipped))
	if err != nil {
		return fmt.Errorf("introSave: %w", err)
	}
	e.arguments = setBooleanLaunchArgument(e.arguments, "skip-intro", isSkipped)
	e.status.IsIntroSkipped = isSkipped
	e.emitStatusLocked()
	return nil
}

// SetSkipCinematic persists the choice before applying it to future launches.
func (e *App) SetSkipCinematic(isSkipped bool) error {
	paths, err := e.spinnerPaths()
	if err != nil {
		return fmt.Errorf("cinematicPath: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	err = writeLauncherConfigLine(paths.configPath, "is_cinematic_skipped", "is_cinematic_skipped = "+strconv.FormatBool(isSkipped))
	if err != nil {
		return fmt.Errorf("cinematicSave: %w", err)
	}
	e.arguments = setBooleanLaunchArgument(e.arguments, "skip-cinematic", isSkipped)
	e.status.IsCinematicSkipped = isSkipped
	e.emitStatusLocked()
	return nil
}
