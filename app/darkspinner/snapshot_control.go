package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Connection credentials are transient process inputs, never saved configuration.
type snapshotControlConnection struct {
	endpoint  string
	token     string
	directory string
}

func inheritedSnapshotControl() snapshotControlConnection {
	return snapshotControlConnection{
		endpoint: os.Getenv(snapshotEndpointEnvironment), token: os.Getenv(snapshotTokenEnvironment),
		directory: os.Getenv(snapshotDirectoryEnvironment),
	}
}

func (e snapshotControlConnection) withDirectory(directory string) snapshotControlConnection {
	if e.directory == "" {
		e.directory = directory
	}
	return e
}

func (e snapshotControlConnection) environment() map[string]string {
	return map[string]string{
		snapshotEndpointEnvironment: e.endpoint, snapshotTokenEnvironment: e.token,
		snapshotDirectoryEnvironment: e.directory,
	}
}

func (e snapshotControlConnection) environmentEntries(entries []string) []string {
	overrides := e.environment()
	resultEntries := make([]string, 0, len(entries))
	for _, entry := range entries {
		separatorIndex := strings.IndexByte(entry, '=')
		name := ""
		if separatorIndex >= 0 {
			name = entry[:separatorIndex]
		}
		isOverridden := false
		for overrideName := range overrides {
			if separatorIndex >= 0 && strings.EqualFold(name, overrideName) {
				isOverridden = true
				break
			}
		}
		if !isOverridden {
			resultEntries = append(resultEntries, entry)
		}
	}
	for name, contents := range overrides {
		resultEntries = append(resultEntries, name+"="+contents)
	}
	return resultEntries
}

// Only the obsolete command file is removed. Captures and diagnostic archives
// remain intact, including reports which still need investigation.
func cleanupLegacySnapshotControls() {
	// Supervisors perform migration once; private worker subprocesses must not
	// repeatedly scan all previous runs on every script/client launch.
	for _, argument := range os.Args[1:] {
		switch argument {
		case "--scenario-worker", "--scenario-script-worker", "--scenario-detached-worker", "--scenario-debugger-worker", "--version", "-version":
			return
		}
	}
	baseDirectory, err := executableDirectory()
	if err != nil {
		log.Printf("Legacy snapshot cleanup: executable directory: %v", err)
		return
	}
	roots := []string{filepath.Join(baseDirectory, "darkspin", "logs"),
		filepath.Join(baseDirectory, "darkspin", "mod", "logs")}
	ancestor := baseDirectory
	for depth := 0; depth < 6; depth++ {
		fi, statErr := os.Lstat(filepath.Join(ancestor, "go.mod"))
		if statErr == nil && fi.Mode().IsRegular() {
			roots = append(roots, filepath.Join(ancestor, "bin", "game", "logs", "scenarios"),
				filepath.Join(ancestor, "bin", "game", "logs"),
				filepath.Join(ancestor, "bin", "game", "darkspin", "logs"),
				filepath.Join(ancestor, "bin", "game", "darkspin", "mod", "logs"),
				filepath.Join(ancestor, "bin", "darkspinner", "darkspin", "logs"),
				filepath.Join(ancestor, "bin", "darkspinner", "darkspin", "mod", "logs"),
				filepath.Join(ancestor, "bin", "server", "darkspin", "logs"))
			break
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			break
		}
		ancestor = parent
	}
	for _, root := range roots {
		err = cleanupLegacySnapshotRoot(root)
		if err != nil {
			log.Printf("Legacy snapshot cleanup at %s: %v", root, err)
		}
	}
}

func cleanupLegacySnapshotRoot(root string) error {
	// Refuse a symlink or junction at any ancestor, including the root itself.
	for ancestor := root; ; ancestor = filepath.Dir(ancestor) {
		fi, err := os.Lstat(ancestor)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("rootStat: %w", err)
		}
		if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return fmt.Errorf("snapshot cleanup skips linked path %s", ancestor)
		}
		if filepath.Dir(ancestor) == ancestor {
			break
		}
	}
	entryCount := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			log.Printf("Legacy snapshot cleanup skips %s: %v", path, walkErr)
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		entryCount++
		if entryCount > 100000 {
			return errors.New("snapshot cleanup entry limit reached")
		}
		if entry.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return nil
		}
		if entry.IsDir() {
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return fmt.Errorf("cleanupRelative: %w", relErr)
			}
			if strings.Count(relative, string(filepath.Separator)) >= 16 {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != "snapshot-control.txt" {
			return nil
		}
		fi, statErr := entry.Info()
		if statErr != nil {
			log.Printf("Legacy snapshot cleanup skips %s: %v", path, statErr)
			return nil
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		removeErr := os.Remove(path)
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			log.Printf("Legacy snapshot cleanup could not remove %s: %v", path, removeErr)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("cleanupWalk: %w", err)
	}
	return nil
}
