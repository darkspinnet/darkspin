//go:build scenario

package jsonstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
)

type Store struct {
	root        string
	mutex       sync.Mutex
	allocations map[string]scenario.RunPaths
}

func New(root string) (*Store, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("storeRoot: %w", err)
	}
	fi, err := os.Stat(absoluteRoot)
	if err != nil {
		return nil, fmt.Errorf("rootStat: %w", err)
	}
	if !fi.IsDir() {
		return nil, errors.New("scenario workspace root must be an existing directory")
	}
	err = checkAncestors(absoluteRoot)
	if err != nil {
		return nil, fmt.Errorf("rootAncestors: %w", err)
	}
	return &Store{root: absoluteRoot, allocations: map[string]scenario.RunPaths{}}, nil
}

func (e *Store) Allocate(ctx context.Context, definition scenario.Definition) (scenario.RunPaths, error) {
	err := definition.Validate()
	if err != nil {
		return scenario.RunPaths{}, fmt.Errorf("allocateDefinition: %w", err)
	}
	err = ctx.Err()
	if err != nil {
		return scenario.RunPaths{}, fmt.Errorf("allocateContext: %w", err)
	}
	secret := make([]byte, 16)
	count, err := rand.Read(secret)
	if err != nil {
		return scenario.RunPaths{}, fmt.Errorf("runRandom: %w", err)
	}
	if count != len(secret) {
		return scenario.RunPaths{}, errors.New("run identity random source returned a short read")
	}
	runID := time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(secret)
	paths := scenario.RunPaths{RunID: runID,
		CacheDirectory:  filepath.Join(e.root, "bin", "cache", "scenario", runID),
		ReportDirectory: filepath.Join(e.root, "bin", "game", "logs", "scenarios", runID),
		TraceDirectory:  filepath.Join(e.root, "bin", "server", "darkspin", "logs", "traces", runID),
	}
	paths.ManifestPath = filepath.Join(paths.ReportDirectory, "manifest.json")
	createdDirectories := []string{}
	for _, directory := range []string{paths.CacheDirectory, paths.ReportDirectory, paths.TraceDirectory} {
		err = e.exclusiveDirectory(directory)
		if err != nil {
			cleanupErr := removeEmptyDirectories(createdDirectories)
			if cleanupErr != nil {
				return scenario.RunPaths{}, fmt.Errorf("allocateCleanup: %w", errors.Join(err, cleanupErr))
			}
			return scenario.RunPaths{}, fmt.Errorf("allocateDirectory: %w", err)
		}
		createdDirectories = append(createdDirectories, directory)
	}
	e.mutex.Lock()
	e.allocations[runID] = paths
	e.mutex.Unlock()
	return paths, nil
}

func (e *Store) exclusiveDirectory(directory string) error {
	err := checkAncestors(filepath.Dir(directory))
	if err != nil {
		return fmt.Errorf("directoryAncestors: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(directory), 0700)
	if err != nil {
		return fmt.Errorf("directoryParents: %w", err)
	}
	err = checkAncestors(filepath.Dir(directory))
	if err != nil {
		return fmt.Errorf("createdAncestors: %w", err)
	}
	// Mkdir, rather than MkdirAll, prohibits reusing any existing run/profile.
	err = os.Mkdir(directory, 0700)
	if err != nil {
		return fmt.Errorf("directoryExclusive: %w", err)
	}
	return nil
}

func (e *Store) Save(ctx context.Context, manifest scenario.Manifest) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	paths, isOwned := e.allocations[manifest.RunID]
	if !isOwned || paths != manifest.Paths {
		return errors.New("manifest paths were not allocated by this store")
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("saveContext: %w", err)
	}
	err = checkAncestors(paths.ReportDirectory)
	if err != nil {
		return fmt.Errorf("saveAncestors: %w", err)
	}
	err = rejectLink(paths.ManifestPath)
	if err != nil {
		return fmt.Errorf("manifestLink: %w", err)
	}
	payload, err := json.MarshalIndent(marshalManifest(manifest), "", "  ")
	if err != nil {
		return fmt.Errorf("manifestMarshal: %w", err)
	}
	payload = append(payload, '\n')
	w, err := os.CreateTemp(paths.ReportDirectory, "manifest-*.partial")
	if err != nil {
		return fmt.Errorf("manifestTemporary: %w", err)
	}
	temporaryPath := w.Name()
	count, writeErr := w.Write(payload)
	if writeErr == nil && count != len(payload) {
		writeErr = errors.New("manifest write returned a short write")
	}
	if writeErr == nil {
		writeErr = w.Sync()
	}
	closeErr := w.Close()
	if writeErr != nil || closeErr != nil {
		removeErr := os.Remove(temporaryPath)
		return fmt.Errorf("manifestWrite: %w", errors.Join(writeErr, closeErr, removeErr))
	}
	err = ctx.Err()
	if err == nil {
		err = os.Rename(temporaryPath, paths.ManifestPath)
	}
	if err != nil {
		removeErr := os.Remove(temporaryPath)
		return fmt.Errorf("manifestReplace: %w", errors.Join(err, removeErr))
	}
	return nil
}

func checkAncestors(path string) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		err := rejectLink(current)
		if err != nil {
			return fmt.Errorf("pathAncestor: %w", err)
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

func rejectLink(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("pathStat: %w", err)
	}
	if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return fmt.Errorf("scenario paths must not traverse a link or irregular reparse point: %s", path)
	}
	return nil
}

func removeEmptyDirectories(directories []string) error {
	var cleanupErr error
	for index := len(directories) - 1; index >= 0; index-- {
		// These exact paths were created by this allocation. No recursive cleanup
		// touches profiles, files or any preexisting shared parent.
		err := os.Remove(directories[index])
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("directoryRemove: %w", err))
		}
	}
	if cleanupErr != nil {
		return fmt.Errorf("allocationRollback: %w", cleanupErr)
	}
	return nil
}

// WorkspaceRoot uses an explicit root or discovers the nearest checkout from
// the caller's current directory. It never treats an arbitrary directory as one.
func WorkspaceRoot(requestedRoot string) (string, error) {
	if strings.TrimSpace(requestedRoot) != "" {
		root, err := filepath.Abs(requestedRoot)
		if err != nil {
			return "", fmt.Errorf("workspaceAbsolute: %w", err)
		}
		return root, nil
	}
	current, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("workspaceCurrent: %w", err)
	}
	for {
		fi, statErr := os.Stat(filepath.Join(current, "go.mod"))
		if statErr == nil && !fi.IsDir() {
			return current, nil
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return "", fmt.Errorf("workspaceMarker: %w", statErr)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("scenario requires --workspace pointing to the Dark Spin checkout")
		}
		current = parent
	}
}
