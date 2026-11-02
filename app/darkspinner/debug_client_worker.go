//go:build scenario && windows

package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
	"golang.org/x/sys/windows"
)

type debugDetachedWorker struct {
	jwt           string
	token         string
	cancel        context.CancelFunc
	startup       *time.Timer
	gameDirectory string
}

// Only the private same-executable worker sets this process-local owner. The
// ordinary launcher path always observes nil and retains its existing behavior.
var activeDebugDetachedWorker *debugDetachedWorker

func runDebugDetachedWorker(arguments []string) (bool, error) {
	isRequested := false
	for _, argument := range arguments {
		if argument == "--scenario-detached-worker" {
			isRequested = true
		}
	}
	if !isRequested {
		return false, nil
	}
	if len(arguments) != 1 {
		return true, errors.New("detached scenario worker accepts only its private bootstrap")
	}
	err := ensureStandardUser()
	if err != nil {
		return true, fmt.Errorf("helperPrivilege: %w", err)
	}
	err = configureDarkSpinnerVersion()
	if err != nil {
		return true, fmt.Errorf("helperVersion: %w", err)
	}
	bootstrap, err := readDebugDetachedMessage(os.Stdin)
	if err != nil {
		return true, fmt.Errorf("workerBootstrap: %w", err)
	}
	if bootstrap.Kind != "bootstrap" || len(bootstrap.Token) < 32 || bootstrap.JWT == "" || len(bootstrap.Arguments) == 0 || bootstrap.Arguments[0] != "--detached-game" {
		return true, errors.New("invalid detached worker bootstrap")
	}
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker := &debugDetachedWorker{jwt: bootstrap.JWT, token: bootstrap.Token, cancel: cancel, gameDirectory: bootstrap.GameDirectory}
	bootstrap.JWT = ""
	bootstrap.Token = ""
	activeDebugDetachedWorker = worker
	worker.startup = time.AfterFunc(30*time.Second, worker.expireStartup)
	defer worker.startup.Stop()
	stopInput := context.AfterFunc(lifetime, worker.closeInput)
	defer stopInput()
	err = runDetachedGameContext(lifetime, bootstrap.Arguments)
	if err != nil {
		return true, fmt.Errorf("workerDetached: %w", err)
	}
	return true, nil
}

func debugDetachedCredential(jwt string) (string, error) {
	worker := activeDebugDetachedWorker
	if worker == nil {
		return jwt, nil
	}
	if jwt != "" {
		return "", errors.New("private detached bootstrap cannot be combined with another credential")
	}
	credential := worker.jwt
	worker.jwt = ""
	normalized, err := normalizeJWT(credential)
	if err != nil {
		return "", fmt.Errorf("workerCredential: %w", err)
	}
	return normalized, nil
}

func debugDetachedLaunchObserver() injectedLaunchObserver {
	if activeDebugDetachedWorker == nil {
		return nil
	}
	return activeDebugDetachedWorker
}

func debugDetachedGameDirectory(directory string) string {
	if activeDebugDetachedWorker == nil {
		return directory
	}
	return activeDebugDetachedWorker.gameDirectory
}

func (e *debugDetachedWorker) BeforeResume(ctx context.Context, process uintptr, moduleHandle uint32, fangPath string) error {
	observer := &scenarioClient{}
	err := observer.BeforeResume(ctx, process, moduleHandle, fangPath)
	if err != nil {
		return fmt.Errorf("workerCapability: %w", err)
	}
	processID, err := windows.GetProcessId(windows.Handle(process))
	if err != nil {
		return fmt.Errorf("workerPID: %w", err)
	}
	capability := observer.capability
	if capability.ProtocolVersion != scenario.ProtocolVersion {
		return errors.New("unsupported detached Fang protocol")
	}
	response := scenarioCapabilityResponse{ProtocolVersion: capability.ProtocolVersion, Component: capability.Component, BuildID: capability.BuildID, Features: capability.Features}
	err = writeDebugDetachedMessage(os.Stdout, debugDetachedMessage{Kind: "ready", Token: e.token, ProcessID: processID, Capability: &response})
	if err != nil {
		return fmt.Errorf("workerReady: %w", err)
	}
	accepted, err := readDebugDetachedMessage(os.Stdin)
	if err != nil {
		return fmt.Errorf("workerAccept: %w", err)
	}
	if accepted.Kind != "accepted" || subtle.ConstantTimeCompare([]byte(accepted.Token), []byte(e.token)) != 1 {
		return errors.New("detached owner did not authenticate the resume handoff")
	}
	e.token = ""
	go e.monitorParent()
	return nil
}

func (e *debugDetachedWorker) AfterResume(ctx context.Context, processID uint32) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("workerResumeContext: %w", err)
	}
	err = writeDebugDetachedMessage(os.Stdout, debugDetachedMessage{Kind: "resumed", ProcessID: processID})
	if err != nil {
		return fmt.Errorf("workerResumed: %w", err)
	}
	e.startup.Stop()
	return nil
}

func (e *debugDetachedWorker) monitorParent() {
	message, err := readDebugDetachedMessage(os.Stdin)
	if err != nil || message.Kind != "keepalive" {
		// Parent EOF or an unsupported control message revokes this helper's
		// lifetime; launchInjected then settles its retained native handle.
		e.cancel()
		return
	}
	// No persistent command channel is advertised in this increment.
	e.cancel()
}

func (e *debugDetachedWorker) expireStartup() { e.cancel() }
func (e *debugDetachedWorker) closeInput() {
	err := os.Stdin.Close()
	if err != nil { /* Cancellation still reaches the owned native process watcher. */
	}
}
