//go:build scenario

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/darkspinnet/darkspin/server/scenario/desktop"
)

// Development runs read the repository directly. Standalone scenario launchers
// read their adjacent disk tree. Neither path installs or caches Lua sources.
func resolveDebugLuaRoot(directory string) (string, error) {
	basePath, err := filepath.Abs(directory)
	if err != nil {
		return "", fmt.Errorf("luaBase: %w", err)
	}
	err = desktop.CheckAncestors(basePath)
	if err != nil {
		return "", fmt.Errorf("luaBaseCheck: %w", err)
	}
	for ancestor := basePath; ; ancestor = filepath.Dir(ancestor) {
		modulePath := filepath.Join(ancestor, "go.mod")
		fi, statErr := os.Lstat(modulePath)
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return "", fmt.Errorf("luaModuleStat: %w", statErr)
		}
		if statErr == nil && fi.Mode().IsRegular() {
			sourceRoot := filepath.Join(ancestor, "app", "darkspinner", "lua")
			sourceFI, sourceErr := os.Lstat(sourceRoot)
			if sourceErr != nil && !errors.Is(sourceErr, os.ErrNotExist) {
				return "", fmt.Errorf("luaSourceStat: %w", sourceErr)
			}
			if sourceErr == nil {
				if !sourceFI.IsDir() {
					return "", errors.New("repository Lua source root must be a directory")
				}
				err = desktop.CheckAncestors(sourceRoot)
				if err != nil {
					return "", fmt.Errorf("luaSourceCheck: %w", err)
				}
				return sourceRoot, nil
			}
		}
		if filepath.Dir(ancestor) == ancestor {
			break
		}
	}
	root := filepath.Join(basePath, "darkspin", "lua")
	err = desktop.CheckAncestors(root)
	if err != nil {
		return "", fmt.Errorf("luaDiskRoot: %w", err)
	}
	fi, err := os.Lstat(root)
	if err != nil {
		return "", fmt.Errorf("luaRootStat: %w", err)
	}
	if !fi.IsDir() {
		return "", errors.New("disk Lua source root must be a directory")
	}
	return root, nil
}
