//go:build scenario && windows

package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

func (e *scenarioClient) Liveness(ctx context.Context) (scenarioClientLiveness, error) {
	observation := scenarioClientLiveness{}
	if ctx == nil || e == nil {
		return observation, errors.New("client liveness requires context and owned client")
	}
	err := lockScenarioLiveness(ctx, &e.mutex)
	if err != nil {
		return observation, fmt.Errorf("livenessClientLock: %w", err)
	}
	defer e.mutex.Unlock()
	if e.process == nil {
		return observation, errors.New("owned client process is closed or unavailable")
	}
	err = lockScenarioLiveness(ctx, &e.process.mutex)
	if err != nil {
		return observation, fmt.Errorf("livenessProcessLock: %w", err)
	}
	defer e.process.mutex.Unlock()
	if e.process.process == 0 || e.process.processID == 0 {
		return observation, errors.New("owned client process handle or identity is unavailable")
	}
	status, err := windows.WaitForSingleObject(e.process.process, 0)
	if err != nil {
		return observation, fmt.Errorf("livenessWait: %w", err)
	}
	observation.ProcessID = e.process.processID
	observation.ObservedAt = time.Now().UTC()
	switch status {
	case uint32(windows.WAIT_TIMEOUT):
		observation.IsAlive = true
	case windows.WAIT_OBJECT_0:
		var exitCode uint32
		err = windows.GetExitCodeProcess(e.process.process, &exitCode)
		if err != nil {
			return scenarioClientLiveness{}, fmt.Errorf("livenessExitCode: %w", err)
		}
		observation.IsExited = true
		observation.ExitCode = &exitCode
	default:
		return scenarioClientLiveness{}, fmt.Errorf("unsupported owned process wait status %d", status)
	}
	err = ctx.Err()
	if err != nil {
		return scenarioClientLiveness{}, fmt.Errorf("livenessContext: %w", err)
	}
	return observation, nil
}

func lockScenarioLiveness(ctx context.Context, ownerMutex *sync.Mutex) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := ctx.Err()
		if err != nil {
			return fmt.Errorf("lockContext: %w", err)
		}
		if ownerMutex.TryLock() {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("lockWait: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
