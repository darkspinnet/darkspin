package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	steamLibraryPathPattern = regexp.MustCompile(`(?i)"path"\s*"([^"]+)"`)
	steamInstallDirPattern  = regexp.MustCompile(`(?i)"installdir"\s*"([^"]+)"`)
)

func discoverSteamLibraryGameRoot(steamRoots []string) string {
	seenLibraryRoot := make(map[string]struct{})
	for _, steamRoot := range steamRoots {
		libraryRoots := []string{steamRoot}
		contents, err := os.ReadFile(filepath.Join(steamRoot, "steamapps", "libraryfolders.vdf"))
		if err == nil {
			libraryRoots = append(libraryRoots, parseSteamLibraryRoots(contents)...)
		}
		for _, libraryRoot := range libraryRoots {
			libraryRoot = filepath.Clean(libraryRoot)
			if _, isSeen := seenLibraryRoot[libraryRoot]; isSeen {
				continue
			}
			seenLibraryRoot[libraryRoot] = struct{}{}
			manifestPath := filepath.Join(libraryRoot, "steamapps", "appmanifest_"+steamDemoAppID+".acf")
			manifest, err := os.ReadFile(manifestPath)
			if err != nil {
				continue
			}
			installDirectory := parseSteamInstallDirectory(manifest)
			if installDirectory == "" {
				continue
			}
			gameRoot := filepath.Join(libraryRoot, "steamapps", "common", installDirectory)
			if isLocalGameRoot(gameRoot) {
				return filepath.Clean(gameRoot)
			}
		}
	}
	return ""
}

func parseSteamLibraryRoots(contents []byte) []string {
	matches := steamLibraryPathPattern.FindAllStringSubmatch(string(contents), -1)
	roots := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) == 2 {
			roots = append(roots, strings.ReplaceAll(match[1], `\\`, `\`))
		}
	}
	return roots
}

func parseSteamInstallDirectory(contents []byte) string {
	match := steamInstallDirPattern.FindStringSubmatch(string(contents))
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}
