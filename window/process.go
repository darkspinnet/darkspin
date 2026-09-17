//go:build windows

// Package window manages application processes, windows, and instances.
package window

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Running returns the process IDs matching executableName.
func Running(executableName string) ([]uint32, error) {
	executableName = strings.TrimSpace(executableName)
	if executableName == "" {
		return nil, errors.New("empty executable name")
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("snapshotCreate: %w", err)
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	err = windows.Process32First(snapshot, &entry)
	if err != nil {
		return nil, fmt.Errorf("processFirst: %w", err)
	}
	processIDs := make([]uint32, 0, 1)
	for {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if strings.EqualFold(name, executableName) {
			processIDs = append(processIDs, entry.ProcessID)
		}
		err = windows.Process32Next(snapshot, &entry)
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("processNext: %w", err)
		}
	}
	return processIDs, nil
}

// IsRunning reports whether executableName has at least one active process.
func IsRunning(executableName string) (bool, error) {
	processIDs, err := Running(executableName)
	if err != nil {
		return false, fmt.Errorf("processList: %w", err)
	}
	return len(processIDs) != 0, nil
}

// StopAll terminates every process matching executableName and waits for exit.
func StopAll(executableName string) error {
	processIDs, err := Running(executableName)
	if err != nil {
		return fmt.Errorf("processList: %w", err)
	}
	for _, processID := range processIDs {
		err = StopProcess(processID)
		if err != nil {
			return fmt.Errorf("processStop[%d]: %w", processID, err)
		}
	}
	return nil
}

// StopProcess terminates one process ID and waits for exit.
func StopProcess(processID uint32) error {
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, processID)
	if err != nil {
		return fmt.Errorf("processOpen: %w", err)
	}
	err = windows.TerminateProcess(handle, 1)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return fmt.Errorf("processTerminate: %w", err)
	}
	waitStatus, waitErr := windows.WaitForSingleObject(handle, 5_000)
	closeErr := windows.CloseHandle(handle)
	if waitErr != nil {
		return fmt.Errorf("processWait: %w", waitErr)
	}
	if waitStatus != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("processWait: status %d", waitStatus)
	}
	if closeErr != nil {
		return fmt.Errorf("processClose: %w", closeErr)
	}
	return nil
}
