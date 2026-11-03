package main

import (
	"errors"
	"fmt"
	"net/http"
)

type RemoteControlAccessResult struct {
	Status              int64  `json:"status"`
	IPLimit             string `json:"ip_limit"`
	IsDisconnectPending bool   `json:"is_disconnect_pending"`
}

func (e *App) SetRemoteControlUserAccess(serverAddress, identity string, userID, status int64, ipLimit, reason string) (RemoteControlAccessResult, error) {
	if userID <= 0 || status < 0 {
		return RemoteControlAccessResult{}, errors.New("controlPanel:invalid_request")
	}
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlAccessResult{}
	path := fmt.Sprintf("/admin/users/%d/access", userID)
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPost, path, map[string]any{
		"status": status, "ip_limit": ipLimit, "reason": reason,
	}, &result)
	if err != nil {
		return RemoteControlAccessResult{}, fmt.Errorf("panelAccess: %w", err)
	}
	return result, nil
}
