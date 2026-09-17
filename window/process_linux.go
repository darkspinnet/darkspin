//go:build linux

// Package window manages application processes, windows, and instances.
package window

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Running returns the process IDs matching executableName.
func Running(executableName string) ([]uint32, error) {
	executableName = strings.TrimSpace(executableName)
	if executableName == "" {
		return nil, errors.New("empty executable name")
	}
	entry, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("procRead: %w", err)
	}
	processID := make([]uint32, 0, 1)
	for _, item := range entry {
		id, parseErr := strconv.ParseUint(item.Name(), 10, 32)
		if parseErr != nil || !item.IsDir() {
			continue
		}
		contents, readErr := os.ReadFile(filepath.Join("/proc", item.Name(), "cmdline"))
		if readErr != nil || !commandMatches(contents, executableName) {
			continue
		}
		processID = append(processID, uint32(id))
	}
	return processID, nil
}

func commandMatches(contents []byte, executableName string) bool {
	for _, argument := range strings.Split(string(contents), "\x00") {
		if strings.EqualFold(filepath.Base(argument), executableName) {
			return true
		}
	}
	return false
}

// IsRunning reports whether executableName has at least one active process.
func IsRunning(executableName string) (bool, error) {
	processID, err := Running(executableName)
	if err != nil {
		return false, fmt.Errorf("processList: %w", err)
	}
	return len(processID) != 0, nil
}

// StopAll terminates every process matching executableName and waits for exit.
func StopAll(executableName string) error {
	processID, err := Running(executableName)
	if err != nil {
		return fmt.Errorf("processList: %w", err)
	}
	for _, id := range processID {
		err = StopProcess(id)
		if err != nil {
			return fmt.Errorf("processStop[%d]: %w", id, err)
		}
	}
	return nil
}

// StopProcess terminates one process ID and waits for exit.
func StopProcess(processID uint32) error {
	err := syscall.Kill(int(processID), syscall.SIGTERM)
	if err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("processSignal: %w", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		err = syscall.Kill(int(processID), 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	err = syscall.Kill(int(processID), 0)
	if err == nil {
		return errors.New("process exit timed out")
	}
	if !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("processWait: %w", err)
	}
	return nil
}
