//go:build mage

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// BuildRunLua builds scenario support and runs a Lua scenario by name or path.
// Example: mage darkspinner:buildrunlua two_arsenal
func (Darkspinner) BuildRunLua(name string) error {
	scriptPath, err := resolveDarkSpinnerLua(name)
	if err != nil {
		return fmt.Errorf("luaResolve: %w", err)
	}
	err = (Scenario{}).Build()
	if err != nil {
		return fmt.Errorf("luaBuild: %w", err)
	}
	binaryName := darkSpinnerBinaryName
	if runtime.GOOS != "windows" {
		binaryName = darkSpinnerLinuxBinaryName
	}
	if runtime.GOOS == "darwin" {
		binaryName = filepath.Join("darkspinner.app", "Contents", "MacOS", "darkspinner")
	}
	binaryPath, err := filepath.Abs(filepath.Join("bin", "game", binaryName))
	if err != nil {
		return fmt.Errorf("luaBinary: %w", err)
	}
	fmt.Printf("Lua scenario: %s\n", scriptPath)
	err = runCommand(filepath.Dir(binaryPath), binaryPath, "--lua", scriptPath)
	if err != nil {
		return fmt.Errorf("luaRun: %w", err)
	}
	return nil
}

// Match the development launcher's repository source root. Runtime copies are
// not an overlay and must never shadow edited source files.
func resolveDarkSpinnerLua(name string) (string, error) {
	name = filepath.FromSlash(strings.TrimSpace(name))
	if name == "" || filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return "", errors.New("supply a scenario name or a path relative to the Lua root")
	}
	name = filepath.Clean(name)
	if name == "." || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
		return "", errors.New("scenario path must stay inside the Lua root")
	}
	if filepath.Ext(name) == "" {
		name += ".lua"
	}
	if filepath.Ext(name) != ".lua" {
		return "", errors.New("scenario must be a .lua source file")
	}
	roots := []string{
		filepath.Join("app", "darkspinner", "lua"),
	}
	for _, root := range roots {
		fi, err := os.Lstat(filepath.Join(root, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("luaStat: %w", err)
		}
		if !fi.Mode().IsRegular() {
			return "", fmt.Errorf("scenario %q is not a regular source file", name)
		}
		return filepath.ToSlash(name), nil
	}
	// A qualified path is explicit; do not silently reinterpret a misspelled
	// directory as a basename somewhere else in the tree.
	if filepath.Dir(name) != "." {
		return "", fmt.Errorf("scenario %q was not found under app/darkspinner/lua", filepath.ToSlash(name))
	}
	search := darkSpinnerLuaSearch{name: name, matchesByPath: make(map[string]bool)}
	for _, root := range roots {
		search.root = root
		err := filepath.WalkDir(root, search.visit)
		if err != nil {
			return "", fmt.Errorf("luaWalk: %w", err)
		}
	}
	matches := make([]string, 0, len(search.matchesByPath))
	for path := range search.matchesByPath {
		matches = append(matches, path)
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return "", fmt.Errorf("scenario %q was not found under app/darkspinner/lua", name)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("scenario %q is ambiguous; supply one of these relative paths: %s", name, strings.Join(matches, ", "))
	}
	return matches[0], nil
}

type darkSpinnerLuaSearch struct {
	root          string
	name          string
	matchesByPath map[string]bool
}

func (e *darkSpinnerLuaSearch) visit(path string, entry fs.DirEntry, walkErr error) error {
	if errors.Is(walkErr, os.ErrNotExist) && path == e.root {
		return nil
	}
	if walkErr != nil {
		return fmt.Errorf("luaEntry: %w", walkErr)
	}
	if !entry.Type().IsRegular() {
		return nil
	}
	if entry.Name() != e.name && (runtime.GOOS != "windows" || !strings.EqualFold(entry.Name(), e.name)) {
		return nil
	}
	relativePath, err := filepath.Rel(e.root, path)
	if err != nil {
		return fmt.Errorf("luaRelative: %w", err)
	}
	e.matchesByPath[filepath.ToSlash(relativePath)] = true
	return nil
}
