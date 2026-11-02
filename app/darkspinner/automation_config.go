package main

import (
	"fmt"
	"strconv"
)

type LauncherAutomationConfiguration struct {
	IsAutoPatchEnabled *bool `json:"isAutoPatchEnabled"`
	IsAutoPlayEnabled  *bool `json:"isAutoPlayEnabled"`
}

func (e *App) GetLauncherAutomationConfiguration() (LauncherAutomationConfiguration, error) {
	paths, err := e.spinnerPaths()
	if err != nil {
		return LauncherAutomationConfiguration{}, fmt.Errorf("automationPath: %w", err)
	}
	settings, err := readLauncherSettings(paths.configPath)
	if err != nil {
		return LauncherAutomationConfiguration{}, fmt.Errorf("automationRead: %w", err)
	}
	return LauncherAutomationConfiguration{
		IsAutoPatchEnabled: settings.IsAutoPatchEnabled,
		IsAutoPlayEnabled:  settings.IsAutoPlayEnabled,
	}, nil
}

func (e *App) SetAutoPatch(isEnabled bool) error {
	paths, err := e.spinnerPaths()
	if err != nil {
		return fmt.Errorf("autoPatchPath: %w", err)
	}
	err = writeLauncherConfigLine(paths.configPath, "is_auto_patch_enabled", "is_auto_patch_enabled = "+strconv.FormatBool(isEnabled))
	if err != nil {
		return fmt.Errorf("autoPatchSave: %w", err)
	}
	return nil
}

func (e *App) SetAutoPlay(isEnabled bool) error {
	paths, err := e.spinnerPaths()
	if err != nil {
		return fmt.Errorf("autoPlayPath: %w", err)
	}
	err = writeLauncherConfigLine(paths.configPath, "is_auto_play_enabled", "is_auto_play_enabled = "+strconv.FormatBool(isEnabled))
	if err != nil {
		return fmt.Errorf("autoPlaySave: %w", err)
	}
	return nil
}
