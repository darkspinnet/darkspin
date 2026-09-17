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
