package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// launcherSettings is the launcher-owned darkspin/launcher.toml. It lives
// apart from darkspin.toml because the server rewrites that file from its own
// fields and would drop launcher keys.
type launcherSettings struct {
	Wine launcherWineSettings `toml:"wine"`
}

type launcherWineSettings struct {
	RuntimePath string `toml:"runtime_path"`
}

func readLauncherSettings(path string) (launcherSettings, error) {
	settings := launcherSettings{}
	r, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, fmt.Errorf("settingOpen: %w", err)
	}
	defer r.Close()
	_, err = toml.NewDecoder(r).Decode(&settings)
	if err != nil {
		return settings, fmt.Errorf("settingDecode: %w", err)
	}
	return settings, nil
}

func writeLauncherSettings(path string, settings launcherSettings) error {
	var encoded bytes.Buffer
	err := toml.NewEncoder(&encoded).Encode(settings)
	if err != nil {
		return fmt.Errorf("settingEncode: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return fmt.Errorf("settingMkdir: %w", err)
	}
	err = replaceLauncherConfig(path, encoded.Bytes())
	if err != nil {
		return fmt.Errorf("settingReplace: %w", err)
	}
	return nil
}

// writeLauncherConfigLine replaces or inserts one `key = ...` line in the
// [launcher] table of darkspin.toml and leaves every other line untouched.
func writeLauncherConfigLine(configPath, key, line string) error {
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
