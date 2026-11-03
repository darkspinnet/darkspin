package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// OpenLocalControlPanel uses the launcher's owned server, without remote
// credentials or activating a gameplay login for the selected local profile.
func (e *App) OpenLocalControlPanel(identity string) (RemoteControlPanel, error) {
	identity = strings.TrimSpace(identity)
	err := validateProfileIdentity(identity)
	if err != nil {
		return RemoteControlPanel{}, fmt.Errorf("panelIdentity: %w", err)
	}
	err = e.SetLocalServicesRequired(true)
	if err != nil {
		return RemoteControlPanel{}, fmt.Errorf("panelServices: %w", err)
	}
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	e.closeRemoteControlPanelLocked()
	e.serverMu.Lock()
	defer e.serverMu.Unlock()
	e.mu.Lock()
	gameServer := e.serviceSet.gameServer
	ctx := e.lifecycleCtx
	isShuttingDown := e.isShuttingDown
	e.mu.Unlock()
	if isShuttingDown || gameServer == nil {
		return RemoteControlPanel{}, errors.New("local server is not online")
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	address, err := localServerAddress(gameServer)
	if err != nil {
		return RemoteControlPanel{}, fmt.Errorf("panelAddress: %w", err)
	}
	e.selectControlActivityOwnerLocked(address, identity)
	token, err := gameServer.OpenLocalControlPanel(ctx, identity)
	if err != nil {
		return RemoteControlPanel{}, fmt.Errorf("panelOpen: %w", err)
	}
	e.controlPanel = remoteControlSession{address: address, identity: identity, token: token, isLocal: true}
	panel, err := e.getRemoteControlPanelLocked(ctx)
	if err != nil {
		return RemoteControlPanel{}, fmt.Errorf("panelInitial: %w", err)
	}
	e.controlPanel.identity = panel.Profile.LoginName
	return panel, nil
}

func localControlOperations(operations []string) []string {
	localOperations := make([]string, 0, len(operations))
	for _, operation := range operations {
		switch operation {
		case "account.rename", "account.password", "account.delete", "account.sessions.revoke":
			// These remote operations require a current account password. Local
			// profile editing remains owned by the existing launcher operations.
			continue
		}
		localOperations = append(localOperations, operation)
	}
	return localOperations
}
