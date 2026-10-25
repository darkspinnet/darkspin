//go:build scenario && !windows

package main

import (
	"context"
	"errors"
	"time"
)

func runDebugPauseWorker(arguments []string) (bool, error) {
	for _, argument := range arguments {
		if argument == "--scenario-debugger-worker" {
			return true, errors.New("debugger pause requires Windows")
		}
	}
	return false, nil
}
func (e *debugClientManager) DebugPauseState(clientID string) (debugPauseState, error) {
	return debugPauseState{}, errors.New("debugger pause requires Windows")
}
func (e *debugClientManager) ResumeDebugPause(ctx context.Context, clientID, token string) (debugPauseState, error) {
	return debugPauseState{}, errors.New("debugger pause requires Windows")
}
func (e *debugClientManager) StartDebugPause(ctx context.Context, host *debugHost, owner debugWorldOwner, duration time.Duration, manifest string) (debugPauseState, error) {
	return debugPauseState{}, errors.New("debugger pause requires Windows")
}
