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
	"github.com/darkspinnet/darkspin/server/game"
)

var launcherConfigMu sync.Mutex

// launcherSettings lives in [launcher] and its subtables in darkspin.toml.
type launcherSettings struct {
	Locale             string               `toml:"locale"`
	IsAutoPatchEnabled *bool                `toml:"is_auto_patch_enabled"`
	IsAutoPlayEnabled  *bool                `toml:"is_auto_play_enabled"`
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
	edit := launcherSettingsEdit{settings: settings}
	err = game.WithConfiguration(path, edit.apply)
	if err != nil {
		return fmt.Errorf("settingSave: %w", err)
	}
	return nil
}

type launcherSettingsEdit struct{ settings launcherSettings }

func (e launcherSettingsEdit) apply(contents []byte) ([]byte, error) {
	sections, err := decodeLauncherSections(contents)
	if err != nil {
		return nil, fmt.Errorf("settingRead: %w", err)
	}
	launcherSections, err := launcherSection(sections, "launcher")
	if err != nil {
		return nil, fmt.Errorf("settingSection: %w", err)
	}
	wineFields, err := launcherSection(launcherSections, "wine")
	if err != nil {
		return nil, fmt.Errorf("wineSection: %w", err)
	}
	modFields, err := launcherSection(launcherSections, "mod")
	if err != nil {
		return nil, fmt.Errorf("modSection: %w", err)
	}
	wineFields["runtime_path"] = e.settings.Wine.RuntimePath
	modFields["spore_path"] = e.settings.Mod.SporePath
	modFields["is_detection_attempted"] = e.settings.Mod.IsDetectionAttempted
	encoded, err := encodeLauncherSections(sections)
	if err != nil {
		return nil, fmt.Errorf("settingEncode: %w", err)
	}
	return encoded, nil
}

// writeLauncherConfigLine replaces or inserts one `key = ...` line in the
// [launcher] table of darkspin.toml and leaves every other line untouched.
func writeLauncherConfigLine(configPath, key, line string) error {
	launcherConfigMu.Lock()
	defer launcherConfigMu.Unlock()
	edit := launcherLineEdit{key: key, line: line}
	err := game.WithConfiguration(configPath, edit.apply)
	if err != nil {
		return fmt.Errorf("launcherLine: %w", err)
	}
	return nil
}

type launcherLineEdit struct{ key, line string }

func (e launcherLineEdit) apply(contents []byte) ([]byte, error) {
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
		if sectionStart >= 0 && isLauncherConfigKeyLine(trimmed, e.key) {
			keyIndex = index
		}
	}
	switch {
	case keyIndex >= 0:
		lines[keyIndex] = e.line
	case sectionStart >= 0:
		lines = append(lines[:sectionEnd], append([]string{e.line}, lines[sectionEnd:]...)...)
	default:
		for len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, "", "[launcher]", e.line, "")
	}
	return []byte(strings.Join(lines, lineEnding)), nil
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
