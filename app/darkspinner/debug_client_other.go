//go:build scenario && !windows

package main

import (
	"context"
	"fmt"
)

type debugClientManager struct{}

func newDebugClientManager(ctx context.Context, runID, cacheDirectory, reportDirectory string) *debugClientManager {
	return &debugClientManager{}
}

func (e *debugClientManager) Launch(ctx context.Context, req debugClientLaunchRequest) (debugClientState, error) {
	return debugClientState{}, fmt.Errorf("owned scenario clients require Windows")
}

func (e *debugClientManager) State(clientID string) (debugClientState, error) {
	return debugClientState{}, fmt.Errorf("owned scenario clients require Windows")
}

func (e *debugClientManager) List() []debugClientState { return nil }

func (e *debugClientManager) Close(ctx context.Context, clientID string, isTerminate bool) error {
	return fmt.Errorf("owned scenario clients require Windows")
}

func (e *debugClientManager) Shutdown(ctx context.Context) error  { return nil }
func (e *debugClientManager) Terminate(ctx context.Context) error { return nil }

func (e *debugClientManager) Forget(clientID string) error {
	return fmt.Errorf("owned scenario clients require Windows")
}

func debugDetachedCredential(jwt string) (string, error) { return jwt, nil }

func debugDetachedGameDirectory(directory string) string { return directory }

func runDebugDetachedWorker(arguments []string) (bool, error) {
	for _, argument := range arguments {
		if argument == "--scenario-detached-worker" {
			return true, fmt.Errorf("detached scenario worker requires Windows")
		}
	}
	return false, nil
}
