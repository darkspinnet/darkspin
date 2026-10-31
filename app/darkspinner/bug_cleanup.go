package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// cleanupOldBugReports expires extracted diagnostic batches from a development
// workspace. Release installations without a bin parent have no such directory.
func cleanupOldBugReports(basePath string) error {
	basePath, err := filepath.Abs(basePath)
	if err != nil {
		return fmt.Errorf("bugBase: %w", err)
	}
	if !strings.EqualFold(filepath.Base(filepath.Dir(basePath)), "bin") {
		return nil
	}
	workspace := filepath.Dir(filepath.Dir(basePath))
	root := filepath.Join(workspace, "bin", "game", "logs", "bugs")
	fi, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("bugRoot: %w", err)
	}
	if !fi.IsDir() {
		return errors.New("bug report root is not a directory")
	}
	for ancestor := root; ; ancestor = filepath.Dir(ancestor) {
		fi, err = os.Lstat(ancestor)
		if err != nil {
			return fmt.Errorf("bugAncestor: %w", err)
		}
		if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return errors.New("bug report path contains a link or reparse point")
		}
		if filepath.Dir(ancestor) == ancestor {
			break
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("bugList: %w", err)
	}
	cutoff := time.Now().Add(-14 * 24 * time.Hour)
	var cleanupErr error
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			continue
		}
		fi, err = entry.Info()
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("bugStat[%s]: %w", entry.Name(), err))
			continue
		}
		if !fi.ModTime().Before(cutoff) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if !strings.EqualFold(filepath.Dir(path), root) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("bugPath[%s]: not an immediate child", entry.Name()))
			continue
		}
		err = os.RemoveAll(path)
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("bugRemove[%s]: %w", entry.Name(), err))
		}
	}
	if cleanupErr != nil {
		return fmt.Errorf("bugCleanup: %w", cleanupErr)
	}
	return nil
}
