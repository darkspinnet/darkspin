//go:build scenario

package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/server/scenario/desktop"
)

// Authored developer sources only. Ordinary binaries do not select this file.
//
//go:embed lua lua/.luarc.json
var debugLuaAssets embed.FS

type debugLuaInstaller struct{ root string }

// Move the entire old developer directory before installing bundled sources.
// Rename preserves custom scripts and dotfiles without rewriting their bytes.
func prepareDebugLuaAssets(directory string) (string, error) {
	basePath, err := filepath.Abs(directory)
	if err != nil {
		return "", fmt.Errorf("luaBase: %w", err)
	}
	root := filepath.Join(basePath, "darkspin", "lua")
	legacyPath := filepath.Join(basePath, "lua")
	err = desktop.CheckAncestors(basePath)
	if err != nil {
		return "", fmt.Errorf("luaBaseCheck: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(root), 0700)
	if err != nil {
		return "", fmt.Errorf("luaParentCreate: %w", err)
	}
	err = desktop.CheckAncestors(filepath.Dir(root))
	if err != nil {
		return "", fmt.Errorf("luaParentCheck: %w", err)
	}
	fi, legacyErr := os.Lstat(legacyPath)
	if legacyErr != nil && !errors.Is(legacyErr, os.ErrNotExist) {
		return "", fmt.Errorf("luaLegacyStat: %w", legacyErr)
	}
	if legacyErr == nil {
		if !fi.IsDir() || fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return "", errors.New("legacy Lua path must be an ordinary directory")
		}
		target, targetErr := os.Lstat(root)
		if targetErr != nil && !errors.Is(targetErr, os.ErrNotExist) {
			return "", fmt.Errorf("luaTargetStat: %w", targetErr)
		}
		if targetErr == nil {
			if !target.IsDir() || target.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
				return "", errors.New("Lua destination must be an ordinary directory")
			}
			// Only remove an empty legacy directory. If both contain scripts,
			// retain them and report the conflict instead of overwriting either.
			err = os.Remove(legacyPath)
			if err != nil {
				return "", fmt.Errorf("luaConflict: %w; merge %s into %s without overwriting scripts", err, legacyPath, root)
			}
		} else {
			err = os.Rename(legacyPath, root)
			if err != nil {
				return "", fmt.Errorf("luaMigrate: %w", err)
			}
		}
	}
	err = installDebugLuaAssets(root)
	if err != nil {
		return "", fmt.Errorf("luaInstall: %w", err)
	}
	return root, nil
}

func installDebugLuaAssets(root string) error {
	err := os.MkdirAll(root, 0700)
	if err != nil {
		return fmt.Errorf("luaRootCreate: %w", err)
	}
	err = desktop.CheckAncestors(root)
	if err != nil {
		return fmt.Errorf("luaRootCheck: %w", err)
	}
	installer := debugLuaInstaller{root: root}
	err = fs.WalkDir(debugLuaAssets, "lua", installer.visit)
	if err != nil {
		return fmt.Errorf("luaAssetsWalk: %w", err)
	}
	return nil
}

func (e *debugLuaInstaller) visit(name string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return fmt.Errorf("luaAssetEntry: %w", walkErr)
	}
	if name == "lua" {
		return nil
	}
	path := filepath.Join(e.root, filepath.FromSlash(strings.TrimPrefix(name, "lua/")))
	if entry.IsDir() {
		err := os.MkdirAll(path, 0700)
		if err != nil {
			return fmt.Errorf("luaAssetDirectory: %w", err)
		}
		err = desktop.CheckAncestors(path)
		if err != nil {
			return fmt.Errorf("luaAssetAncestors: %w", err)
		}
		return nil
	}
	err := desktop.CheckAncestors(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("luaAssetParent: %w", err)
	}
	w, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("luaAssetCreate: %w", err)
	}
	payload, readErr := debugLuaAssets.ReadFile(name)
	if readErr != nil {
		closeErr := w.Close()
		return fmt.Errorf("luaAssetRead: %w", errors.Join(readErr, closeErr))
	}
	count, writeErr := w.Write(payload)
	closeErr := w.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("luaAssetWrite: %w", errors.Join(writeErr, closeErr))
	}
	if count != len(payload) {
		return errors.New("short developer Lua asset write")
	}
	return nil
}
