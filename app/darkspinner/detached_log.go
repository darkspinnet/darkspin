package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A fresh launcher is the retention boundary for per-instance detached logs
// and obsolete default chat/protocol files. SQL chat history and snapshot
// capture are independent of these files.
// Detached children do not call this; their current logs remain available to
// /bug and Send report throughout the launcher session.
func cleanupDetachedLogs(basePath string) error {
	logDirectory := filepath.Join(basePath, "darkspin", "logs")
	launchErr := cleanupDetachedLogDirectory(logDirectory, "detached-launch-", ".log")
	traceErr := cleanupDetachedLogDirectory(filepath.Join(logDirectory, "traces"), "game-detached-", ".jsonl")
	resultDirectory := filepath.Join(logDirectory, clientFailureDirectory)
	resultErr := cleanupDetachedLogDirectory(resultDirectory, "", ".json")
	temporaryErr := cleanupDetachedLogDirectory(resultDirectory, "", ".tmp")
	consoleErr := cleanupPreviousLuaConsoleLogs(basePath)
	bugErr := cleanupOldBugReports(basePath)
	var legacyErr error
	legacyPaths := []string{
		filepath.Join(logDirectory, "chat.log"),
		filepath.Join(basePath, "darkspin", "mod", "logs", "chat.log"),
		filepath.Join(logDirectory, "traces", "server.jsonl"),
		filepath.Join(basePath, "darkspin", "mod", "logs", "traces", "server.jsonl"),
	}
	for _, path := range legacyPaths {
		err := os.Remove(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			legacyErr = errors.Join(legacyErr, fmt.Errorf("legacyRemove: %w", err))
		}
	}
	if launchErr != nil || traceErr != nil || resultErr != nil || temporaryErr != nil || consoleErr != nil || bugErr != nil || legacyErr != nil {
		return fmt.Errorf("clientLogs: %w", errors.Join(launchErr, traceErr, resultErr, temporaryErr, consoleErr, bugErr, legacyErr))
	}
	return nil
}

func cleanupPreviousLuaConsoleLogs(basePath string) error {
	directory := filepath.Join(basePath, "logs", "scenarios")
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("consoleList: %w", err)
	}
	var cleanupErr error
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 || !strings.HasPrefix(entry.Name(), "launcher-") {
			continue
		}
		identity, decodeErr := hex.DecodeString(strings.TrimPrefix(entry.Name(), "launcher-"))
		if decodeErr != nil || len(identity) != 12 {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		err = os.Remove(filepath.Join(path, "console.log"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("consoleRemove[%s]: %w", entry.Name(), err))
			continue
		}
		// Remove only an empty legacy directory; never recurse into artifacts.
		err = os.Remove(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("consoleDirectory[%s]: %w", entry.Name(), err))
		}
	}
	if cleanupErr != nil {
		return fmt.Errorf("consoleCleanup: %w", cleanupErr)
	}
	return nil
}

func cleanupDetachedLogDirectory(directory, prefix, suffix string) error {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("logList: %w", err)
	}
	var cleanupErr error
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}
		err = os.Remove(filepath.Join(directory, entry.Name()))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("logRemove[%s]: %w", entry.Name(), err))
		}
	}
	if cleanupErr != nil {
		return fmt.Errorf("logCleanup: %w", cleanupErr)
	}
	return nil
}
