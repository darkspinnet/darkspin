package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/darkspinnet/darkspin/server/game"
)

func migrateLauncherSettings(configPath string) error {
	launcherConfigMu.Lock()
	defer launcherConfigMu.Unlock()
	err := migrateLauncherSettingsLocked(configPath)
	if err != nil {
		return fmt.Errorf("settingsMigrate: %w", err)
	}
	return nil
}

func migrateLauncherSettingsLocked(configPath string) error {
	// Establish the full defaults even when a launcher preference is the first
	// operation after installation, before the embedded server has started.
	config, isCreated, err := game.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("configPrepare: %w", err)
	}
	if config == nil {
		return fmt.Errorf("configuration missing after preparation (created=%t)", isCreated)
	}
	basePath := filepath.Dir(configPath)
	legacyPaths := []string{
		filepath.Join(basePath, "darkspin", "launcher.toml"),
		filepath.Join(basePath, "launcher.toml"),
	}
	for _, legacyPath := range legacyPaths {
		contents, err := os.ReadFile(legacyPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("legacyRead: %w", err)
		}
		var settings launcherSettings
		err = toml.Unmarshal(contents, &settings)
		if err != nil {
			return fmt.Errorf("legacyValidate: %w", err)
		}
		legacySections := make(map[string]any)
		err = toml.Unmarshal(contents, &legacySections)
		if err != nil {
			return fmt.Errorf("legacyDecode: %w", err)
		}
		sections, readErr := readLauncherSections(configPath)
		if readErr != nil {
			return fmt.Errorf("configRead: %w", readErr)
		}
		launcherSections, sectionErr := launcherSection(sections, "launcher")
		if sectionErr != nil {
			return fmt.Errorf("configSection: %w", sectionErr)
		}
		mergeLauncherSections(launcherSections, legacySections)
		err = saveLauncherSections(configPath, sections)
		if err != nil {
			return fmt.Errorf("migrationSave: %w", err)
		}
		// The synced atomic replacement must succeed before deleting the source.
		err = os.Remove(legacyPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("legacyRemove: %w", err)
		}
	}
	return nil
}

// Explicit new settings win, including empty paths and false booleans. Missing
// fields inherit legacy settings so interrupted migrations are safe to repeat.
func mergeLauncherSections(fields, legacyFields map[string]any) {
	for key, legacyField := range legacyFields {
		field, isPresent := fields[key]
		if !isPresent {
			fields[key] = legacyField
			continue
		}
		childFields, isTable := field.(map[string]any)
		legacyChildFields, isLegacyTable := legacyField.(map[string]any)
		if isTable && isLegacyTable {
			mergeLauncherSections(childFields, legacyChildFields)
		}
	}
}

func readLauncherSections(path string) (map[string]any, error) {
	sections := make(map[string]any)
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return sections, nil
	}
	if err != nil {
		return nil, fmt.Errorf("configRead: %w", err)
	}
	err = toml.Unmarshal(contents, &sections)
	if err != nil {
		return nil, fmt.Errorf("configDecode: %w", err)
	}
	return sections, nil
}

func launcherSection(sections map[string]any, name string) (map[string]any, error) {
	section, isPresent := sections[name]
	if !isPresent {
		fields := make(map[string]any)
		sections[name] = fields
		return fields, nil
	}
	fields, isTable := section.(map[string]any)
	if !isTable {
		return nil, fmt.Errorf("%s must be a TOML table", name)
	}
	return fields, nil
}

func saveLauncherSections(path string, sections map[string]any) error {
	var encoded bytes.Buffer
	err := toml.NewEncoder(&encoded).Encode(sections)
	if err != nil {
		return fmt.Errorf("configEncode: %w", err)
	}
	var document struct {
		Launcher launcherSettings `toml:"launcher"`
	}
	err = toml.Unmarshal(encoded.Bytes(), &document)
	if err != nil {
		return fmt.Errorf("launcherValidate: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return fmt.Errorf("configMkdir: %w", err)
	}
	err = replaceLauncherConfig(path, encoded.Bytes())
	if err != nil {
		return fmt.Errorf("configReplace: %w", err)
	}
	return nil
}
