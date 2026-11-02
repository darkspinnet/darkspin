package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	appwindow "github.com/darkspinnet/darkspin/window"
)

// ResetProfileDisplay restores a selected client's display to 720p windowed mode.
// A remote address selects the host-specific local client profile.
func (a *App) ResetProfileDisplay(identity, serverAddress string) error {
	identity = strings.TrimSpace(identity)
	err := validateProfileIdentity(identity)
	if err != nil {
		return fmt.Errorf("displayIdentity: %w", err)
	}
	if serverAddress != "" {
		serverAddress, err = normalizeRemoteServerAddress(serverAddress)
		if err != nil {
			return fmt.Errorf("displayServer: %w", err)
		}
		host, port, splitErr := net.SplitHostPort(serverAddress)
		if splitErr != nil {
			return fmt.Errorf("displayHost: %w", splitErr)
		}
		if port == "" {
			return errors.New("remote server port is required")
		}
		identity += "@" + host
	}
	err = a.resetClientProfileDisplay(identity)
	if err != nil {
		return fmt.Errorf("displayReset: %w", err)
	}
	return nil
}

// ResetModProfileDisplay resets the separate client settings used by mod launches.
func (e *App) ResetModProfileDisplay(identity string) error {
	e.modMu.Lock()
	defer e.modMu.Unlock()
	identity = strings.TrimSpace(identity)
	err := validateProfileIdentity(identity)
	if err != nil {
		return fmt.Errorf("modIdentity: %w", err)
	}
	err = e.requireModIdle()
	if err != nil {
		return fmt.Errorf("modIdle: %w", err)
	}
	err = e.resetClientProfileDisplay(modClientProfile(identity))
	if err != nil {
		return fmt.Errorf("modDisplay: %w", err)
	}
	return nil
}

func (e *App) resetClientProfileDisplay(identity string) error {
	isRunning, err := appwindow.IsRunning(gameProcessName)
	if err != nil {
		return fmt.Errorf("displayProcess: %w", err)
	}
	if isRunning {
		return errors.New("close the game before resetting display settings")
	}
	e.mu.Lock()
	isLaunching := e.cancel != nil || e.gameDone != nil
	e.mu.Unlock()
	if isLaunching {
		return errors.New("finish the active game launch before resetting display settings")
	}
	basePath, err := executableDirectory()
	if err != nil {
		return fmt.Errorf("displayBase: %w", err)
	}
	profilePath, err := prepareClientConfig(basePath, identity)
	if err != nil {
		return fmt.Errorf("displayProfile: %w", err)
	}
	err = ensureWindowedClientPreference(profilePath)
	if err != nil {
		return fmt.Errorf("displaySeed: %w", err)
	}
	preferencePath := filepath.Join(profilePath, "AppData", "Roaming", "DarksporeData", "Preferences", "Preferences.prop")
	preference, err := os.ReadFile(preferencePath)
	if err != nil {
		return fmt.Errorf("displayRead: %w", err)
	}
	for _, setting := range []struct{ key, setting string }{
		{"OptionFullScreen", "0"},
		{"ScreenSizeWidth", "1280"},
		{"ScreenSizeHeight", "720"},
		{"ScreenSizeRefresh", "60"},
	} {
		preference = replaceDisplaySetting(preference, setting.key, " ", setting.setting)
	}
	err = os.WriteFile(preferencePath, preference, 0o600)
	if err != nil {
		return fmt.Errorf("displayPreference: %w", err)
	}
	displayPath := filepath.Join(profilePath, "AppData", "Roaming", "DarksporeData", "Preferences", "DarkspinDisplay.ini")
	display, err := os.ReadFile(displayPath)
	if errors.Is(err, os.ErrNotExist) {
		display = []byte("[display]\r\n")
	} else if err != nil {
		return fmt.Errorf("displayConfigRead: %w", err)
	}
	display = replaceDisplaySetting(display, "is_borderless_mode", "=", "0")
	err = os.WriteFile(displayPath, display, 0o600)
	if err != nil {
		return fmt.Errorf("displayConfigWrite: %w", err)
	}
	return nil
}

func replaceDisplaySetting(contents []byte, key, separator, setting string) []byte {
	normalized := strings.TrimRight(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n")
	lines := []string{}
	if normalized != "" {
		lines = strings.Split(normalized, "\n")
	}
	isFound := false
	for index, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), key+separator) {
			continue
		}
		lines[index] = key + separator + setting
		isFound = true
	}
	if !isFound {
		lines = append(lines, key+separator+setting)
	}
	return []byte(strings.TrimRight(strings.Join(lines, "\r\n"), "\r\n") + "\r\n")
}

func seedCrogenitorDisplay(identity string) error {
	basePath, err := executableDirectory()
	if err != nil {
		return fmt.Errorf("displayBase: %w", err)
	}
	profilePath, err := prepareClientConfig(basePath, identity)
	if err != nil {
		return fmt.Errorf("displayProfile: %w", err)
	}
	err = ensureWindowedClientPreference(profilePath)
	if err != nil {
		return fmt.Errorf("displayPreference: %w", err)
	}
	preferencePath := filepath.Join(profilePath, "AppData", "Roaming", "DarksporeData", "Preferences", "Preferences.prop")
	currentPreference, err := os.ReadFile(preferencePath)
	if err != nil {
		return fmt.Errorf("displayRead: %w", err)
	}
	settings := []string{
		"OptionShadows 3", "OptionEffects 3", "OptionGlow 3", "OptionFog 3",
		"OptionToonLining 3", "OptionDetailPlayer 3", "OptionDetailWorld 3",
		"OptionFootsteps 3", "OptionDistortion 3", "OptionGrass 3",
		"OptionCloudShadows 1", "OptionSpotLightShadows 1", "OptionAntialias 1",
	}
	width, height, refresh, displayErr := primaryDisplayMode()
	if displayErr == nil {
		settings = append(settings,
			fmt.Sprintf("ScreenSizeWidth %d", width),
			fmt.Sprintf("ScreenSizeHeight %d", height),
			fmt.Sprintf("ScreenSizeRefresh %d", refresh),
		)
	} else {
		// Keep the client's own resolution choice when the primary mode is unavailable.
	}
	missingPreference := ""
	for _, setting := range settings {
		key, _, isKey := strings.Cut(setting, " ")
		if !isKey {
			return fmt.Errorf("displaySetting: missing key in %q", setting)
		}
		if !strings.Contains("\n"+string(currentPreference), "\n"+key+" ") {
			missingPreference += setting + "\r\n"
		}
	}
	if missingPreference != "" {
		if len(currentPreference) > 0 && currentPreference[len(currentPreference)-1] != '\n' {
			currentPreference = append(currentPreference, '\r', '\n')
		}
		currentPreference = append(currentPreference, missingPreference...)
		err = os.WriteFile(preferencePath, currentPreference, 0o600)
		if err != nil {
			return fmt.Errorf("displayWrite: %w", err)
		}
	}
	displayPath := filepath.Join(profilePath, "AppData", "Roaming", "DarksporeData", "Preferences", "DarkspinDisplay.ini")
	w, err := os.OpenFile(displayPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("displayCreate: %w", err)
	}
	const displayPreference = "[display]\r\nis_borderless_mode=1\r\nis_taskbar_overlay=1\r\nnative_ui_scale_percent=100\r\n"
	written, writeErr := w.WriteString(displayPreference)
	closeErr := w.Close()
	if writeErr != nil {
		return fmt.Errorf("displaySave: %w", errors.Join(writeErr, closeErr))
	}
	if closeErr != nil {
		return fmt.Errorf("displayClose: %w", closeErr)
	}
	if written != len(displayPreference) {
		return fmt.Errorf("displaySize: wrote %d of %d bytes", written, len(displayPreference))
	}
	return nil
}
