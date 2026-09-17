//go:build linux

package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func discoverSteamRoots() []string {
	rootSet := make(map[string]struct{})
	addSteamRoot(rootSet, os.Getenv("STEAM_COMPAT_CLIENT_INSTALL_PATH"))
	addSteamRoot(rootSet, os.Getenv("STEAM_ROOT"))
	homePath, err := os.UserHomeDir()
	if err == nil {
		dataPath := os.Getenv("XDG_DATA_HOME")
		if strings.TrimSpace(dataPath) == "" {
			dataPath = filepath.Join(homePath, ".local", "share")
		}
		for _, root := range []string{
			filepath.Join(homePath, ".steam", "steam"),
			filepath.Join(homePath, ".steam", "root"),
			filepath.Join(dataPath, "Steam"),
			filepath.Join(dataPath, "steam"),
			filepath.Join(homePath, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
			filepath.Join(homePath, ".var", "app", "com.valvesoftware.Steam", ".steam", "steam"),
			filepath.Join(homePath, "snap", "steam", "common", ".local", "share", "Steam"),
		} {
			addSteamRoot(rootSet, root)
		}
	}

	roots := make([]string, 0, len(rootSet))
	for root := range rootSet {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots
}

func addSteamRoot(rootSet map[string]struct{}, path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return
	}
	absolutePath = filepath.Clean(absolutePath)
	fi, err := os.Stat(filepath.Join(absolutePath, "steamapps"))
	if err != nil || !fi.IsDir() {
		return
	}
	resolvedPath, err := filepath.EvalSymlinks(absolutePath)
	if err == nil {
		absolutePath = filepath.Clean(resolvedPath)
	}
	rootSet[absolutePath] = struct{}{}
}

func discoverSteamGameRoot(steamRoots []string) string {
	return discoverSteamLibraryGameRoot(steamRoots)
}
