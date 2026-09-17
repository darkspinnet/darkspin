//go:build linux || darwin

package window

import (
	"errors"
	"fmt"
	"math"
	"syscall"
	"time"
)

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
	if processID == 0 || processID > math.MaxInt32 {
		return fmt.Errorf("processID: %d outside positive PID range", processID)
	}
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
