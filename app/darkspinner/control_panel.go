package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RemoteControlPanel contains only allowlisted display data, never the bearer token.
type RemoteControlPanel struct {
	AccountID              int64                       `json:"account_id"`
	ServerAddress          string                      `json:"server_address"`
	ExpiresAt              string                      `json:"expires_at"`
	Profile                remoteControlProfile        `json:"profile"`
	Capabilities           remoteControlCapabilities   `json:"capabilities"`
	Server                 remoteControlServer         `json:"server"`
	Announcements          []RemoteControlAnnouncement `json:"announcements"`
	RequestedMaintenanceID string                      `json:"requested_maintenance_id"`
}

type remoteControlProfile struct {
	LoginName   string `json:"login_name"`
	DisplayName string `json:"display_name"`
	AvatarID    uint32 `json:"avatar_id"`
}

type remoteControlCapabilities struct {
	IsAdmin bool `json:"is_admin"`
}
type remoteControlServer struct {
	Name                string                   `json:"name"`
	Description         string                   `json:"description"`
	Welcome             string                   `json:"welcome"`
	Rules               string                   `json:"rules"`
	ContactURL          string                   `json:"contact_url"`
	HealthStatus        string                   `json:"health_status"`
	Maintenance         RemoteControlMaintenance `json:"maintenance"`
	Version             string                   `json:"version"`
	StartedAt           string                   `json:"started_at"`
	OnlineAccountCount  int                      `json:"online_account_count"`
	SupportedOperations []string                 `json:"supported_operations"`
}
type remoteControlSession struct {
	accountID                int64
	address, identity, token string
	isLocal                  bool
	expiresAt                time.Time
}

func (e *App) OpenRemoteControlPanel(serverAddress, identity, password string) (RemoteControlPanel, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	e.closeRemoteControlPanelLocked()
	address, err := normalizeRemoteServerAddress(serverAddress)
	if err != nil {
		return RemoteControlPanel{}, fmt.Errorf("panelAddress: %w", err)
	}
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return RemoteControlPanel{}, errors.New("identity is required")
	}
	e.selectControlActivityOwnerLocked(address, identity)
	if password == "" {
		password, err = e.loadRemotePassword(address, identity)
		if err != nil {
			return RemoteControlPanel{}, fmt.Errorf("panelPassword: %w", err)
		}
	}
	if password == "" {
		return RemoteControlPanel{}, errors.New("password is required")
	}
	ctx := e.lifecycleCtx
	if ctx == nil {
		ctx = context.TODO()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err = requireRemoteCompatibility(ctx, address)
	if err != nil {
		return RemoteControlPanel{}, fmt.Errorf("panelCompatibility: %w", err)
	}
	created := struct {
		Token string `json:"token"`
		RemoteControlPanel
	}{}
	err = remoteControlRequest(ctx, address, "", http.MethodPost, "/session", map[string]string{"account": identity, "password": password}, &created)
	if err != nil {
		return RemoteControlPanel{}, fmt.Errorf("panelSignIn: %w", err)
	}
	if created.Token == "" {
		return RemoteControlPanel{}, errors.New("panel session token is missing")
	}
	e.controlPanel = remoteControlSession{address: address, identity: created.Profile.LoginName, token: created.Token}
	panel, err := e.getRemoteControlPanelLocked(ctx)
	if err != nil {
		return RemoteControlPanel{}, fmt.Errorf("panelInitial: %w", err)
	}
	return panel, nil
}

func (e *App) GetRemoteControlPanel(serverAddress, identity string) (RemoteControlPanel, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	address, err := normalizeRemoteServerAddress(serverAddress)
	if err != nil {
		return RemoteControlPanel{}, fmt.Errorf("panelAddress: %w", err)
	}
	if e.controlPanel.address != address || e.controlPanel.identity != strings.TrimSpace(identity) {
		return RemoteControlPanel{}, errors.New("panel session does not match the selected account")
	}
	ctx := e.lifecycleCtx
	if ctx == nil {
		ctx = context.TODO()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	panel, err := e.getRemoteControlPanelLocked(ctx)
	if err != nil {
		return RemoteControlPanel{}, fmt.Errorf("panelRefresh: %w", err)
	}
	return panel, nil
}

func (e *App) getRemoteControlPanelLocked(ctx context.Context) (RemoteControlPanel, error) {
	panel := RemoteControlPanel{}
	if e.controlPanel.token == "" {
		return panel, errors.New("panel sign-in is required")
	}
	for _, projection := range []struct {
		path        string
		destination any
	}{
		{"/session", &panel}, {"/server", &panel.Server},
	} {
		err := remoteControlRequest(ctx, e.controlPanel.address, e.controlPanel.token, http.MethodGet, projection.path, nil, projection.destination)
		if err != nil {
			e.closeRemoteControlPanelLocked()
			return RemoteControlPanel{}, fmt.Errorf("panelRead: %w", err)
		}
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, panel.ExpiresAt)
	if err != nil {
		e.closeRemoteControlPanelLocked()
		return RemoteControlPanel{}, fmt.Errorf("panelExpiry: %w", err)
	}
	e.controlPanel.expiresAt = expiresAt
	e.controlPanel.accountID = panel.AccountID
	panel.ServerAddress = e.controlPanel.address
	if e.controlPanel.isLocal {
		panel.Server.SupportedOperations = localControlOperations(panel.Server.SupportedOperations)
	}
	if e.controlOperation.address == e.controlPanel.address && e.controlOperation.identity == e.controlPanel.identity {
		panel.RequestedMaintenanceID = e.controlOperation.id
	}
	for _, operation := range panel.Server.SupportedOperations {
		if operation != "server.announcements.read" {
			continue
		}
		announcements := RemoteControlAnnouncementList{}
		err := remoteControlRequest(ctx, e.controlPanel.address, e.controlPanel.token, http.MethodGet, "/server/announcements", nil, &announcements)
		if err != nil {
			e.closeRemoteControlPanelLocked()
			return RemoteControlPanel{}, fmt.Errorf("announcementRead: %w", err)
		}
		panel.Announcements = announcements.Announcements
		break
	}
	return panel, nil
}

func (e *App) CloseRemoteControlPanel() {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	e.closeRemoteControlPanelLocked()
}

func (e *App) closeRemoteControlPanelLocked() {
	session := e.controlPanel
	e.controlPanel = remoteControlSession{}
	if session.token == "" {
		return
	}
	ctx := e.lifecycleCtx
	if ctx == nil {
		ctx = context.TODO()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := remoteControlRequest(ctx, session.address, session.token, http.MethodDelete, "/session", nil, nil)
	if err != nil {
		e.log("Remote control panel sign-out could not be confirmed")
	}
}

func remoteControlRequest(ctx context.Context, address, token, method, path string, req, result any) error {
	var payload []byte
	if req != nil {
		encoded, err := json.Marshal(req)
		if err != nil {
			return fmt.Errorf("panelMarshal: %w", err)
		}
		payload = encoded
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://"+address+"/api/control"+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("panelRequest: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Darkspin-Client-Version", Version)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := remoteHTTPClient(10 * time.Second)
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("panelSend: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return remoteControlResponseError(response)
	}
	if result == nil {
		return nil
	}
	maximumResponseSize := int64(32 * 1024)
	if path == "/server/activity" || path == "/account/activity" || path == "/admin/users/activity" {
		// Bound a year of account, population, receipt, and reward history without
		// truncating valid responses as more allowlisted graph series are added.
		maximumResponseSize = 512 * 1024
	}
	if path == "/server/activity" {
		// Five daily queue series accompany the other bounded year-long graphs.
		maximumResponseSize = 2 * 1024 * 1024
	}
	err = json.NewDecoder(io.LimitReader(response.Body, maximumResponseSize)).Decode(result)
	if err != nil {
		return fmt.Errorf("panelDecode: %w", err)
	}
	return nil
}

// remoteControlResponseError never reflects arbitrary remote response bodies.
func remoteControlResponseError(response *http.Response) error {
	failure := struct {
		Code string `json:"code"`
	}{}
	decodeErr := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&failure)
	if decodeErr == nil {
		switch failure.Code {
		case "unauthorized", "forbidden", "version_mismatch", "rate_limited", "unavailable", "target_online", "name_in_use", "invalid_password", "invalid_name", "admin_protected", "conflict", "invalid_request", "not_found", "username_required", "username_pattern", "username_too_long", "password_required", "password_too_long", "outcome_unknown", "maintenance_conflict", "maintenance_stopping", "maintenance_unsupported", "update_unavailable", "update_unsupported", "update_check_unsupported", "diagnostic_too_large", "diagnostic_invalid_request", "diagnostic_not_found", "diagnostic_unavailable", "diagnostic_conflict":
			return fmt.Errorf("controlPanel:%s", failure.Code)
		}
	}
	// Do not reflect arbitrary server bodies, which may contain credentials.
	return fmt.Errorf("control panel request returned HTTP %d", response.StatusCode)
}
