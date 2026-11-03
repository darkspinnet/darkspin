package main

import (
	"fmt"
	"net/http"
	"strings"
)

// RemoteControlSessionInfo contains display metadata, never a bearer token.
type RemoteControlSessionInfo struct {
	ID         string `json:"id"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at"`
	ExpiresAt  string `json:"expires_at"`
	IsCurrent  bool   `json:"is_current"`
}

type RemoteControlSessionList struct {
	Sessions []RemoteControlSessionInfo `json:"sessions"`
}

type RemoteControlSessionResult struct {
	IsCurrent    bool `json:"is_current"`
	RevokedCount int  `json:"revoked_count"`
}

func (e *App) ListRemoteControlSessions(serverAddress, identity string) (RemoteControlSessionList, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlSessionList{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodGet, "/account/sessions", nil, &result)
	if err != nil {
		return RemoteControlSessionList{}, fmt.Errorf("sessionList: %w", err)
	}
	return result, nil
}

func (e *App) RevokeRemoteControlSession(serverAddress, identity, currentPassword, sessionID string) (RemoteControlSessionResult, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlSessionResult{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPost, "/account/sessions/revoke",
		map[string]string{"id": sessionID, "current_password": currentPassword}, &result)
	if err != nil {
		if strings.Contains(err.Error(), "panelSend:") || strings.Contains(err.Error(), "panelDecode:") {
			return RemoteControlSessionResult{}, fmt.Errorf("controlPanel:outcome_unknown: %w", err)
		}
		return RemoteControlSessionResult{}, fmt.Errorf("sessionRevoke: %w", err)
	}
	if result.IsCurrent {
		e.controlPanel = remoteControlSession{}
	}
	return result, nil
}

func (e *App) RevokeOtherRemoteControlSessions(serverAddress, identity, currentPassword string) (RemoteControlSessionResult, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlSessionResult{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPost, "/account/sessions/revoke-others",
		map[string]string{"current_password": currentPassword}, &result)
	if err != nil {
		if strings.Contains(err.Error(), "panelSend:") || strings.Contains(err.Error(), "panelDecode:") {
			return RemoteControlSessionResult{}, fmt.Errorf("controlPanel:outcome_unknown: %w", err)
		}
		return RemoteControlSessionResult{}, fmt.Errorf("sessionRevokeOthers: %w", err)
	}
	return result, nil
}
