package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

var launcherConfigMu sync.Mutex

// launcherSettings lives in [launcher] and its subtables in darkspin.toml.
type launcherSettings struct {
	Wine               launcherWineSettings `toml:"wine"`
	Mod                launcherModSettings  `toml:"mod"`
	IsIntroSkipped     bool                 `toml:"is_intro_skipped"`
	IsCinematicSkipped bool                 `toml:"is_cinematic_skipped"`
}

type launcherModSettings struct {
	SporePath            string `toml:"spore_path"`
	IsDetectionAttempted bool   `toml:"is_detection_attempted"`
}

type launcherWineSettings struct {
	RuntimePath string `toml:"runtime_path"`
}

func readLauncherSettings(path string) (launcherSettings, error) {
	launcherConfigMu.Lock()
	defer launcherConfigMu.Unlock()
	settings := launcherSettings{}
	err := migrateLauncherSettingsLocked(path)
	if err != nil {
		return settings, fmt.Errorf("settingMigrate: %w", err)
	}
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, fmt.Errorf("settingOpen: %w", err)
	}
	var document struct {
		Launcher launcherSettings `toml:"launcher"`
	}
	err = toml.Unmarshal(contents, &document)
	if err != nil {
		return settings, fmt.Errorf("settingDecode: %w", err)
	}
	return document.Launcher, nil
}

func writeLauncherSettings(path string, settings launcherSettings) error {
	launcherConfigMu.Lock()
	defer launcherConfigMu.Unlock()
	err := migrateLauncherSettingsLocked(path)
	if err != nil {
		return fmt.Errorf("settingMigrate: %w", err)
	}
	sections, err := readLauncherSections(path)
	if err != nil {
		return fmt.Errorf("settingRead: %w", err)
	}
	launcherSections, err := launcherSection(sections, "launcher")
	if err != nil {
		return fmt.Errorf("settingSection: %w", err)
	}
	wineFields, err := launcherSection(launcherSections, "wine")
	if err != nil {
		return fmt.Errorf("wineSection: %w", err)
	}
	modFields, err := launcherSection(launcherSections, "mod")
	if err != nil {
		return fmt.Errorf("modSection: %w", err)
	}
	wineFields["runtime_path"] = settings.Wine.RuntimePath
	modFields["spore_path"] = settings.Mod.SporePath
	modFields["is_detection_attempted"] = settings.Mod.IsDetectionAttempted
	err = saveLauncherSections(path, sections)
	if err != nil {
		return fmt.Errorf("settingSave: %w", err)
	}
	return nil
}

// writeLauncherConfigLine replaces or inserts one `key = ...` line in the
// [launcher] table of darkspin.toml and leaves every other line untouched.
func writeLauncherConfigLine(configPath, key, line string) error {
	launcherConfigMu.Lock()
	defer launcherConfigMu.Unlock()
	contents, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("launcherConfigRead: %w", err)
	}
	lineEnding := "\n"
	if bytes.Contains(contents, []byte("\r\n")) {
		lineEnding = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n")
	sectionStart := -1
	sectionEnd := len(lines)
	keyIndex := -1
	for index, candidate := range lines {
		trimmed := strings.TrimSpace(candidate)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if sectionStart >= 0 {
				sectionEnd = index
				break
			}
			if strings.EqualFold(trimmed, "[launcher]") {
				sectionStart = index
			}
			continue
		}
		if sectionStart >= 0 && isLauncherConfigKeyLine(trimmed, key) {
			keyIndex = index
		}
	}
	switch {
	case keyIndex >= 0:
		lines[keyIndex] = line
	case sectionStart >= 0:
		lines = append(lines[:sectionEnd], append([]string{line}, lines[sectionEnd:]...)...)
	default:
		for len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, "", "[launcher]", line, "")
	}
	err = replaceLauncherConfig(configPath, []byte(strings.Join(lines, lineEnding)))
	if err != nil {
		return fmt.Errorf("launcherConfigReplace: %w", err)
	}
	return nil
}

func isLauncherConfigKeyLine(trimmedLine, key string) bool {
	if !strings.HasPrefix(strings.ToLower(trimmedLine), strings.ToLower(key)) {
		return false
	}
	remainder := strings.TrimSpace(trimmedLine[len(key):])
	return strings.HasPrefix(remainder, "=")
}

func replaceLauncherConfig(configPath string, contents []byte) error {
	directory := filepath.Dir(configPath)
	r, err := os.CreateTemp(directory, ".darkspin-*.toml")
	if err != nil {
		return fmt.Errorf("configTemp: %w", err)
	}
	tempPath := r.Name()
	defer os.Remove(tempPath)
	_, err = r.Write(contents)
	if err != nil {
		_ = r.Close()
		return fmt.Errorf("configWrite: %w", err)
	}
	err = r.Sync()
	if err != nil {
		_ = r.Close()
		return fmt.Errorf("configSync: %w", err)
	}
	err = r.Close()
	if err != nil {
		return fmt.Errorf("configClose: %w", err)
	}
	err = os.Rename(tempPath, configPath)
	if err != nil {
		return fmt.Errorf("configRename: %w", err)
	}
	return nil
}
