//go:build mage

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/magefile/mage/mg"
)

// Temp manages generated build caches and runtime diagnostics.
type Temp mg.Namespace

// Clean removes files last modified more than 30 days ago from bin/cache and
// development log directories, then prunes empty directories. Skips scenario runs and links.
func (Temp) Clean() error {
	err := cleanTemporaryFiles(false)
	if err != nil {
		return fmt.Errorf("tempClean: %w", err)
	}
	return nil
}

// Preview lists the stale files Clean would remove without deleting anything.
func (Temp) Preview() error {
	err := cleanTemporaryFiles(true)
	if err != nil {
		return fmt.Errorf("tempPreview: %w", err)
	}
	return nil
}

type temporaryFileCleanup struct {
	root        string
	cutoff      time.Time
	isPreview   bool
	directories []string
	fileCount   int
	byteCount   int64
}

func cleanTemporaryFiles(isPreview bool) error {
	// Keep the scope explicit: installed game assets, binaries, profiles,
	// configuration and saves are outside these disposable directories.
	roots := []string{
		filepath.Join("bin", "cache"),
		filepath.Join("bin", "game", "logs"),
		filepath.Join("bin", "game", "darkspin", "logs"),
		filepath.Join("bin", "server", "darkspin", "logs"),
		filepath.Join("bin", "darkspinner", "darkspin", "logs"),
	}
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	for _, root := range roots {
		err := cleanTemporaryRoot(root, cutoff, isPreview)
		if err != nil {
			return fmt.Errorf("tempRoot[%s]: %w", root, err)
		}
	}
	return nil
}

func cleanTemporaryRoot(directory string, cutoff time.Time, isPreview bool) error {
	root, err := filepath.Abs(directory)
	if err != nil {
		return fmt.Errorf("tempPath: %w", err)
	}
	// Reject linked ancestors as well as linked entries; never clean a target
	// redirected outside the workspace by a symlink or Windows reparse point.
	for ancestor := root; ; ancestor = filepath.Dir(ancestor) {
		fi, statErr := os.Lstat(ancestor)
		if errors.Is(statErr, os.ErrNotExist) && ancestor == root {
			fmt.Printf("Skipping missing directory: %s\n", root)
			return nil
		}
		if statErr != nil {
			return fmt.Errorf("tempAncestor: %w", statErr)
		}
		if !fi.IsDir() || fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return fmt.Errorf("tempAncestor: not an ordinary directory: %s", ancestor)
		}
		if filepath.Dir(ancestor) == ancestor {
			break
		}
	}
	cleanup := temporaryFileCleanup{root: root, cutoff: cutoff, isPreview: isPreview}
	fmt.Printf("Temporary file cleanup: %s\nFiles modified before: %s\n", root, cutoff.Format(time.RFC3339))
	err = filepath.WalkDir(root, cleanup.visit)
	if err != nil {
		return fmt.Errorf("tempWalk: %w", err)
	}
	directoryCount := 0
	if !isPreview {
		for index := len(cleanup.directories) - 1; index >= 0; index-- {
			directory := cleanup.directories[index]
			entries, readErr := os.ReadDir(directory)
			if readErr != nil {
				return fmt.Errorf("tempRead: %w", readErr)
			}
			if len(entries) != 0 {
				continue
			}
			err = removeTemporaryEntry(directory)
			if err != nil {
				return fmt.Errorf("tempPrune: %w", err)
			}
			directoryCount++
		}
	}
	action := "Removed"
	if isPreview {
		action = "Would remove"
	}
	fmt.Printf("%s %d files (%.2f MiB); pruned %d empty directories. Scenario runs and links are preserved.\n",
		action, cleanup.fileCount, float64(cleanup.byteCount)/(1024*1024), directoryCount)
	return nil
}

func (e *temporaryFileCleanup) visit(path string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return fmt.Errorf("tempEntry: %w", walkErr)
	}
	if path == e.root {
		return nil
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("tempStat: %w", err)
	}
	if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		if entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}
	if entry.IsDir() {
		// Scenario directories require ownership-aware shutdown and cleanup.
		if path == filepath.Join(e.root, "scenarios") || path == filepath.Join(e.root, "scenario") {
			return filepath.SkipDir
		}
		e.directories = append(e.directories, path)
		return nil
	}
	if !fi.Mode().IsRegular() || !fi.ModTime().Before(e.cutoff) {
		return nil
	}
	if !e.isPreview {
		err = removeTemporaryEntry(path)
		if err != nil {
			return fmt.Errorf("tempRemove: %w", err)
		}
	}
	fmt.Println(path)
	e.fileCount++
	e.byteCount += fi.Size()
	return nil
}

// Go's module cache marks files and directories read-only. On Windows even an
// empty read-only directory cannot be removed until that attribute is cleared.
func removeTemporaryEntry(path string) error {
	err := os.Remove(path)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("tempDelete: %w", err)
	}
	fi, statErr := os.Lstat(path)
	if statErr != nil {
		return fmt.Errorf("tempRetryStat: %w", statErr)
	}
	if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || fi.Mode().Perm()&0o200 != 0 {
		return fmt.Errorf("tempPermission: %w", err)
	}
	err = os.Chmod(path, fi.Mode().Perm()|0o200)
	if err != nil {
		return fmt.Errorf("tempWritable: %w", err)
	}
	err = os.Remove(path)
	if err != nil {
		restoreErr := os.Chmod(path, fi.Mode().Perm())
		return fmt.Errorf("tempRetryDelete: %w", errors.Join(err, restoreErr))
	}
	return nil
}
