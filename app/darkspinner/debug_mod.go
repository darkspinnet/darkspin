//go:build scenario

package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	creationreader "github.com/darkspinnet/darkspin/content/creation"
	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
)

// Scenario mods use fresh scenario accounts and the existing native Mod hooks.
// They never open the launcher's persistent Mod-profile save database.
func prepareDebugMod(ctx context.Context, host *debugHost, cacheDirectory string, req debugLaunchRequest) (string, error) {
	if !debugModClientName(req.ClientID) {
		return "", errors.New("mod client ID must be a lowercase name of at most 32 letters, digits, underscores or hyphens")
	}
	store, err := contentsqlite.New(ctx, host.ContentPath)
	if err != nil {
		return "", fmt.Errorf("modContent: %w", err)
	}
	heroes, readErr := store.CreatureTemplates(ctx)
	closeErr := store.Close()
	if readErr != nil || closeErr != nil {
		return "", fmt.Errorf("modHeroes: %w", errors.Join(readErr, closeErr))
	}
	if len(heroes) == 0 || len(heroes) > 128 {
		return "", errors.New("mod editor requires between 1 and 128 installed hero templates")
	}
	manifest := "[mod]\nversion=1\nmodel_baseline=1\nmodel_material=1\nmodel_audio=1\nmodel_scale=1\nmodel_fit=1\nmodel_portrait=1\neditor_music=1\ncharacter_override=1\ncreature_editor=1\n"
	for index, hero := range heroes {
		manifest += fmt.Sprintf("editor_hero%d=%d\n", index+1, hero.ID)
	}
	// An explicit source must validate. Otherwise use the launcher's configured
	// installation or detection; stock-hero body editing also works without it.
	sourcePath := req.SporePath
	if sourcePath == "" {
		paths, pathErr := resolveSpinnerPaths(host.GameDirectory)
		if pathErr != nil {
			return "", fmt.Errorf("modPaths: %w", pathErr)
		}
		settings, settingErr := readLauncherSettings(paths.configPath)
		if settingErr != nil {
			return "", fmt.Errorf("modSettings: %w", settingErr)
		}
		sourcePath = settings.Mod.SporePath
		if sourcePath == "" {
			sourcePath = detectSporeInstallation()
		}
	}
	if sourcePath != "" {
		sourcePath, err = creationreader.Installation(sourcePath)
		if err != nil {
			return "", fmt.Errorf("modSource: %w", err)
		}
		manifest += "editor_scene=1\n"
		for index, name := range []string{"Spore_Game.package", "Spore_Graphics.package", "Spore_Content.package", "Spore_Audio1.package", "Spore_Audio2.package"} {
			path := filepath.Join(sourcePath, "Data", name)
			if strings.ContainsAny(path, "\r\n\x00") {
				return "", errors.New("mod package path contains a control character")
			}
			fi, statErr := os.Stat(path)
			if statErr != nil {
				return "", fmt.Errorf("modPackage[%d]: %w", index, statErr)
			}
			if !fi.Mode().IsRegular() || fi.Size() == 0 {
				return "", errors.New("mod package is not a nonempty regular file")
			}
			slot := index + 1
			if index >= 3 {
				slot = index + 4
			}
			manifest += fmt.Sprintf("package%d=%s\n", slot, path)
		}
	}
	directory := filepath.Join(cacheDirectory, "mod")
	err = os.MkdirAll(directory, 0700)
	if err != nil {
		return "", fmt.Errorf("modDirectory: %w", err)
	}
	path := filepath.Join(directory, req.ClientID+".ini")
	units := utf16.Encode([]rune(manifest))
	payload := make([]byte, 2+len(units)*2)
	payload[0], payload[1] = 0xff, 0xfe
	for index, unit := range units {
		binary.LittleEndian.PutUint16(payload[2+index*2:], unit)
	}
	w, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", fmt.Errorf("modManifest: %w", err)
	}
	writeCount, writeErr := w.Write(payload)
	closeErr = w.Close()
	if writeErr != nil || closeErr != nil {
		return "", fmt.Errorf("modWrite: %w", errors.Join(writeErr, closeErr))
	}
	if writeCount != len(payload) {
		return "", errors.New("short mod manifest write")
	}
	return path, nil
}

func debugModClientName(name string) bool {
	if len(name) == 0 || len(name) > 32 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, character := range name {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}
