//go:build linux || darwin

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func isWineRunnerSupported() bool {
	return true
}

// wineRunnerSet collects distinct installed runtimes keyed by runtime root.
type wineRunnerSet struct {
	pathSet   *spinnerPathSet
	seenPaths map[string]struct{}
	runners   []WineRunner
}

// detectWineRunners lists the system Wine plus Wine builds installed by Lutris,
// Heroic and Bottles, official Steam Proton builds in every library, and custom
// Proton builds such as GE-Proton under compatibilitytools.d.
func detectWineRunners(pathSet *spinnerPathSet) []WineRunner {
	runnerSet := &wineRunnerSet{pathSet: pathSet, seenPaths: make(map[string]struct{})}
	runnerSet.addSystemWine()
	homePath, err := os.UserHomeDir()
	if err == nil {
		homePath = filepath.Clean(homePath)
		runnerSet.addWineChildren(filepath.Join(homePath, ".local", "share", "lutris", "runners", "wine"))
		runnerSet.addWineChildren(filepath.Join(homePath, ".var", "app", "net.lutris.Lutris", "data", "lutris", "runners", "wine"))
		runnerSet.addWineChildren(filepath.Join(homePath, ".config", "heroic", "tools", "wine"))
		runnerSet.addWineChildren(filepath.Join(homePath, ".var", "app", "com.heroicgameslauncher.hgl", "config", "heroic", "tools", "wine"))
		runnerSet.addWineChildren(filepath.Join(homePath, ".local", "share", "bottles", "runners"))
		runnerSet.addWineChildren(filepath.Join(homePath, ".var", "app", "com.usebottles.bottles", "data", "bottles", "runners"))
		runnerSet.addProtonChildren(filepath.Join(homePath, ".config", "heroic", "tools", "proton"))
		runnerSet.addProtonChildren(filepath.Join(homePath, ".var", "app", "com.heroicgameslauncher.hgl", "config", "heroic", "tools", "proton"))
	}
	runnerSet.addWineChildren("/opt")
	steamRoots := discoverSteamRoots()
	for _, libraryRoot := range steamLibraryRoots(steamRoots) {
		runnerSet.addProtonChildren(filepath.Join(libraryRoot, "steamapps", "common"))
	}
	for _, steamRoot := range steamRoots {
		runnerSet.addProtonChildren(filepath.Join(steamRoot, "compatibilitytools.d"))
	}
	sort.SliceStable(runnerSet.runners[1:], func(left, right int) bool {
		leftRunner := runnerSet.runners[1+left]
		rightRunner := runnerSet.runners[1+right]
		if leftRunner.Kind != rightRunner.Kind {
			return leftRunner.Kind < rightRunner.Kind
		}
		return strings.ToLower(leftRunner.Label) < strings.ToLower(rightRunner.Label)
	})
	return runnerSet.runners
}

func (e *wineRunnerSet) addSystemWine() {
	winePath, err := exec.LookPath("wine")
	if err != nil {
		e.runners = append(e.runners, WineRunner{
			Kind: "Wine", Label: "System Wine", Version: "not installed", IsMissing: true,
		})
		return
	}
	e.runners = append(e.runners, WineRunner{
		Kind: "Wine", Label: "System Wine", Version: wineVersion(winePath),
	})
}

// wineVersion asks a Wine loader for its version without starting a prefix.
func wineVersion(winePath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, winePath, "--version")
	command.Env = replaceEnvironment(os.Environ(), "WINEDEBUG", "-all")
	contents, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(contents))
}

func (e *wineRunnerSet) addWineChildren(parentPath string) {
	entries, err := os.ReadDir(parentPath)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if parentPath == "/opt" && !strings.HasPrefix(strings.ToLower(entry.Name()), "wine") {
			continue
		}
		runtimePath := filepath.Join(parentPath, entry.Name())
		// Checking bin/wine follows symlinked runtime directories too.
		if !isWineRunnerDirectory(runtimePath) {
			continue
		}
		e.add(WineRunner{Path: runtimePath, Kind: "Wine", Label: entry.Name()})
	}
}

func (e *wineRunnerSet) addProtonChildren(parentPath string) {
	entries, err := os.ReadDir(parentPath)
	if err != nil {
		return
	}
	for _, entry := range entries {
		protonPath := filepath.Join(parentPath, entry.Name())
		// Stat follows symlinked Proton directories; files and broken links fail.
		fi, statErr := os.Stat(filepath.Join(protonPath, "proton"))
		if statErr != nil || fi.IsDir() {
			continue
		}
		for _, runtimeDirectory := range []string{"files", "dist"} {
			runtimePath := filepath.Join(protonPath, runtimeDirectory)
			if !isWineRunnerDirectory(runtimePath) {
				continue
			}
			e.add(WineRunner{
				Path: runtimePath, Kind: "Proton", Label: entry.Name(),
				Version: readProtonVersion(filepath.Join(protonPath, "version")),
			})
			break
		}
	}
}

func (e *wineRunnerSet) add(runner WineRunner) {
	runner.Path = normalizeWineRunnerPath(runner.Path)
	if runner.Path == "" {
		return
	}
	if _, isSeen := e.seenPaths[runner.Path]; isSeen {
		return
	}
	e.seenPaths[runner.Path] = struct{}{}
	runner.PrefixPath = wineRunnerPrefixPath(e.pathSet, runner.Path)
	e.runners = append(e.runners, runner)
}

func normalizeWineRunnerPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	resolvedPath, err := filepath.EvalSymlinks(absolutePath)
	if err != nil {
		return filepath.Clean(absolutePath)
	}
	return filepath.Clean(resolvedPath)
}

// steamLibrarySet collects distinct Steam library folders.
type steamLibrarySet struct {
	seenPaths map[string]struct{}
	roots     []string
}

func (e *steamLibrarySet) add(path string) {
	path = normalizeWineRunnerPath(path)
	if path == "" {
		return
	}
	if _, isSeen := e.seenPaths[path]; isSeen {
		return
	}
	fi, err := os.Stat(filepath.Join(path, "steamapps"))
	if err != nil || !fi.IsDir() {
		return
	}
	e.seenPaths[path] = struct{}{}
	e.roots = append(e.roots, path)
}

// steamLibraryRoots expands each Steam root into every library folder it lists.
func steamLibraryRoots(steamRoots []string) []string {
	librarySet := &steamLibrarySet{seenPaths: make(map[string]struct{})}
	for _, steamRoot := range steamRoots {
		librarySet.add(steamRoot)
		contents, err := os.ReadFile(filepath.Join(steamRoot, "steamapps", "libraryfolders.vdf"))
		if err != nil {
			continue
		}
		for _, libraryRoot := range parseSteamLibraryRoots(contents) {
			librarySet.add(libraryRoot)
		}
	}
	return librarySet.roots
}

// readProtonVersion returns the last token of a Proton version file, which
// drops the build timestamp Proton writes before its version name.
func readProtonVersion(path string) string {
	contents, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	firstLine, _, _ := strings.Cut(strings.TrimSpace(string(contents)), "\n")
	fields := strings.Fields(firstLine)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}
