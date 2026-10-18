package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WineRunner is one installed Wine or Proton build that can start the game.
type WineRunner struct {
	// Path is the runtime root holding bin/wine; empty is the system Wine on
	// PATH.
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Label      string `json:"label"`
	Version    string `json:"version"`
	PrefixPath string `json:"prefixPath"`
	IsMissing  bool   `json:"isMissing"`
}

// WineRunnerConfiguration lists installed runtimes and the persisted selection.
type WineRunnerConfiguration struct {
	IsSupported  bool         `json:"isSupported"`
	SelectedPath string       `json:"selectedPath"`
	Runners      []WineRunner `json:"runners"`
}

// wineLaunchEnvironment is the resolved runtime and prefix for one game launch.
// A zero value keeps the automatic behavior: system Wine with its own default
// prefix.
type wineLaunchEnvironment struct {
	prefixPath string
	runnerPath string
	isProton   bool
}

// wineRunnerCache keeps the last RESCAN result so opening the Config tab
// never runs runtime detection on its own. Guard it with App.mu.
type wineRunnerCache struct {
	isScanned bool
	runners   []WineRunner
}

// GetWineRunnerConfiguration returns the persisted selection and the runtimes
// found by the last scan. It never detects runtimes itself.
func (e *App) GetWineRunnerConfiguration() (WineRunnerConfiguration, error) {
	configuration := WineRunnerConfiguration{IsSupported: isWineRunnerSupported()}
	if !configuration.IsSupported {
		return configuration, nil
	}
	pathSet, err := e.spinnerPaths()
	if err != nil {
		return WineRunnerConfiguration{}, fmt.Errorf("configPath: %w", err)
	}
	settings, err := readLauncherSettings(pathSet.settingPath)
	if err != nil {
		return WineRunnerConfiguration{}, fmt.Errorf("settingRead: %w", err)
	}
	configuration.SelectedPath = settings.Wine.RuntimePath
	e.mu.Lock()
	configuration.Runners = append([]WineRunner(nil), e.wineRunnerCache.runners...)
	isScanned := e.wineRunnerCache.isScanned
	e.mu.Unlock()
	if !isScanned {
		configuration.Runners = []WineRunner{{Kind: "Wine", Label: "System Wine"}}
	}
	if findWineRunner(configuration.Runners, configuration.SelectedPath) != nil {
		return configuration, nil
	}
	configuration.Runners = append(configuration.Runners, WineRunner{
		Path:       configuration.SelectedPath,
		Kind:       "Wine",
		Label:      wineRunnerLabel(configuration.SelectedPath),
		PrefixPath: wineRunnerPrefixPath(pathSet, configuration.SelectedPath),
		IsMissing:  !isWineRunnerDirectory(configuration.SelectedPath),
	})
	return configuration, nil
}

// ScanWineRunners detects installed Wine and Proton builds, remembers them for
// later Config tab visits, and returns the refreshed configuration.
func (e *App) ScanWineRunners() (WineRunnerConfiguration, error) {
	if !isWineRunnerSupported() {
		return WineRunnerConfiguration{IsSupported: false}, nil
	}
	pathSet, err := e.spinnerPaths()
	if err != nil {
		return WineRunnerConfiguration{}, fmt.Errorf("configPath: %w", err)
	}
	runners := detectWineRunners(pathSet)
	e.mu.Lock()
	e.wineRunnerCache = wineRunnerCache{isScanned: true, runners: runners}
	e.mu.Unlock()
	configuration, err := e.GetWineRunnerConfiguration()
	if err != nil {
		return WineRunnerConfiguration{}, fmt.Errorf("runnerReload: %w", err)
	}
	return configuration, nil
}

// SetWineRunner persists the runtime used for the next game launch. An empty
// path selects the system Wine on PATH.
func (e *App) SetWineRunner(path string) (WineRunnerConfiguration, error) {
	if !isWineRunnerSupported() {
		return WineRunnerConfiguration{}, errors.New("Wine runtime selection is only available on Linux and macOS")
	}
	pathSet, err := e.spinnerPaths()
	if err != nil {
		return WineRunnerConfiguration{}, fmt.Errorf("configPath: %w", err)
	}
	path = strings.TrimSpace(path)
	if path != "" {
		path = filepath.Clean(path)
		if !isWineRunnerDirectory(path) {
			return WineRunnerConfiguration{}, fmt.Errorf("Wine runtime %s has no bin/wine; rescan and choose a listed runtime", path)
		}
	}
	settings, err := readLauncherSettings(pathSet.settingPath)
	if err != nil {
		return WineRunnerConfiguration{}, fmt.Errorf("settingRead: %w", err)
	}
	settings.Wine.RuntimePath = path
	err = writeLauncherSettings(pathSet.settingPath, settings)
	if err != nil {
		return WineRunnerConfiguration{}, fmt.Errorf("settingSave: %w", err)
	}
	configuration, err := e.GetWineRunnerConfiguration()
	if err != nil {
		return WineRunnerConfiguration{}, fmt.Errorf("runnerReload: %w", err)
	}
	return configuration, nil
}

// wineRunnerLabel names a runtime root by its build directory, skipping the
// files or dist layer Proton keeps its Wine tree under.
func wineRunnerLabel(runnerPath string) string {
	name := filepath.Base(runnerPath)
	if name == "files" || name == "dist" {
		name = filepath.Base(filepath.Dir(runnerPath))
	}
	return name
}

// resolveWineLaunch turns the persisted runtime selection into the launch
// environment and makes sure its launcher-owned prefix directory exists.
func resolveWineLaunch(pathSet *spinnerPathSet) (wineLaunchEnvironment, error) {
	if !isWineRunnerSupported() {
		return wineLaunchEnvironment{}, nil
	}
	settings, err := readLauncherSettings(pathSet.settingPath)
	if err != nil {
		return wineLaunchEnvironment{}, fmt.Errorf("settingRead: %w", err)
	}
	selectedPath := settings.Wine.RuntimePath
	if selectedPath == "" {
		return wineLaunchEnvironment{}, nil
	}
	if !isWineRunnerDirectory(selectedPath) {
		return wineLaunchEnvironment{}, fmt.Errorf(
			"configured Wine runtime %s is no longer installed; choose another runtime in Config", selectedPath,
		)
	}
	prefixPath := wineRunnerPrefixPath(pathSet, selectedPath)
	err = os.MkdirAll(prefixPath, 0o755)
	if err != nil {
		return wineLaunchEnvironment{}, fmt.Errorf("prefixMkdir: %w", err)
	}
	return wineLaunchEnvironment{
		prefixPath: prefixPath, runnerPath: selectedPath, isProton: isProtonRuntime(selectedPath),
	}, nil
}

// isProtonRuntime reports whether a runtime root is the files or dist tree of
// a Proton build, which carries the proton script beside it.
func isProtonRuntime(runnerPath string) bool {
	fi, err := os.Stat(filepath.Join(filepath.Dir(runnerPath), "proton"))
	if err != nil {
		return false
	}
	return !fi.IsDir()
}

func findWineRunner(runners []WineRunner, path string) *WineRunner {
	for index := range runners {
		if runners[index].Path == path {
			return &runners[index]
		}
	}
	return nil
}

func isWineRunnerDirectory(path string) bool {
	fi, err := os.Stat(filepath.Join(path, "bin", "wine"))
	if err != nil {
		return false
	}
	return !fi.IsDir()
}

// wineRunnerPrefixPath is the launcher-owned prefix for one runtime. System
// Wine keeps its own default prefix, so it returns an empty path.
func wineRunnerPrefixPath(pathSet *spinnerPathSet, runnerPath string) string {
	if runnerPath == "" {
		return ""
	}
	name := wineRunnerLabel(runnerPath)
	slug := strings.Builder{}
	for _, character := range strings.ToLower(name) {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '.' {
			slug.WriteRune(character)
			continue
		}
		slug.WriteRune('-')
	}
	name = strings.Trim(slug.String(), "-.")
	if name == "" {
		name = "runtime"
	}
	return filepath.Join(pathSet.runtimePath, "prefix", name)
}
