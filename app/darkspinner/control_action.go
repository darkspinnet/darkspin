package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type RemoteControlUser struct {
	ID               int64  `json:"id"`
	LoginName        string `json:"login_name"`
	DisplayName      string `json:"display_name"`
	Level            int64  `json:"level"`
	Status           int64  `json:"status"`
	IPLimit          string `json:"ip_limit"`
	IsBanned         bool   `json:"is_banned"`
	IsTimedOut       bool   `json:"is_timed_out"`
	TimeoutUntilUnix int64  `json:"timeout_until_unix"`
	IsOnline         bool   `json:"is_online"`
}
type RemoteControlUserList struct {
	Users      []RemoteControlUser `json:"users"`
	NextUserID int64               `json:"next_user_id"`
}
type RemoteControlEvent struct {
	ID             int64  `json:"id"`
	ActorType      string `json:"actor_type"`
	ActorUserID    int64  `json:"actor_user_id"`
	TargetUserID   int64  `json:"target_user_id"`
	Action         string `json:"action"`
	OccurredAtUnix int64  `json:"occurred_at_unix"`
	Reason         string `json:"reason"`
	OperationID    string `json:"operation_id"`
	Outcome        string `json:"outcome"`
}
type RemoteControlHistory struct {
	Events      []RemoteControlEvent `json:"events"`
	NextEventID int64                `json:"next_event_id"`
}
type RemoteControlActionResult struct {
	DisplayName         string `json:"display_name"`
	IsBanned            bool   `json:"is_banned"`
	IsTimedOut          bool   `json:"is_timed_out"`
	TimeoutUntilUnix    int64  `json:"timeout_until_unix"`
	IsDisconnectPending bool   `json:"is_disconnect_pending"`
	IsCacheUpdated      bool   `json:"is_cache_updated"`
}

func (e *App) ListRemoteControlUsers(serverAddress, identity, search string, afterUserID int64) (RemoteControlUserList, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlUserList{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPost, "/admin/users/list", map[string]any{"after_user_id": afterUserID, "limit": 25, "search": search}, &result)
	if err != nil {
		return RemoteControlUserList{}, fmt.Errorf("panelUsers: %w", err)
	}
	return result, nil
}

func (e *App) ListRemoteControlHistory(serverAddress, identity string, beforeEventID, targetUserID int64) (RemoteControlHistory, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlHistory{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPost, "/admin/history/list", map[string]any{"before_event_id": beforeEventID, "target_user_id": targetUserID, "limit": 25}, &result)
	if err != nil {
		return RemoteControlHistory{}, fmt.Errorf("panelHistory: %w", err)
	}
	return result, nil
}

func (e *App) ModerateRemoteControlUser(serverAddress, identity string, targetUserID int64, action, reason string) (RemoteControlActionResult, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	if targetUserID < 1 || (action != "ban" && action != "unban" && action != "kick") {
		return RemoteControlActionResult{}, errors.New("invalid moderation command")
	}
	result := RemoteControlActionResult{}
	path := fmt.Sprintf("/admin/users/%d/%s", targetUserID, action)
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPost, path, map[string]string{"reason": reason}, &result)
	if err != nil {
		return RemoteControlActionResult{}, fmt.Errorf("panelModerate: %w", err)
	}
	return result, nil
}

func (e *App) TimeoutRemoteControlUser(serverAddress, identity string, targetUserID, durationMinutes int64, reason string) (RemoteControlActionResult, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	if targetUserID < 1 || durationMinutes < 1 || durationMinutes > 525600 {
		return RemoteControlActionResult{}, errors.New("invalid timeout command")
	}
	result := RemoteControlActionResult{}
	path := fmt.Sprintf("/admin/users/%d/timeout", targetUserID)
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPost, path, map[string]any{
		"reason": reason, "duration_minutes": durationMinutes,
	}, &result)
	if err != nil {
		return RemoteControlActionResult{}, fmt.Errorf("panelTimeout: %w", err)
	}
	return result, nil
}

func (e *App) RenameRemoteControlAccount(serverAddress, identity, currentPassword, displayName string) (RemoteControlActionResult, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlActionResult{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPatch, "/account", map[string]string{"current_password": currentPassword, "display_name": displayName}, &result)
	if err != nil {
		return RemoteControlActionResult{}, fmt.Errorf("panelRename: %w", err)
	}
	err = e.updateRemoteControlCacheLocked(result.DisplayName, false)
	result.IsCacheUpdated = err == nil
	if err != nil {
		e.log("Remote display name changed, but the launcher cache could not be updated")
	}
	return result, nil
}

func (e *App) ChangeRemoteControlPassword(serverAddress, identity, currentPassword, newPassword string) (RemoteControlActionResult, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPost, "/account/password", map[string]string{"current_password": currentPassword, "new_password": newPassword}, nil)
	if err != nil {
		return RemoteControlActionResult{}, fmt.Errorf("panelPassword: %w", err)
	}
	err = e.updateRemoteControlCacheLocked("", true)
	e.controlPanel = remoteControlSession{}
	if err != nil {
		e.log("Remote password changed, but the launcher cache could not be cleared")
	}
	return RemoteControlActionResult{IsCacheUpdated: err == nil}, nil
}

func (e *App) DeleteRemoteControlAccount(serverAddress, identity, currentPassword string) (RemoteControlActionResult, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodDelete, "/account", map[string]string{"current_password": currentPassword}, nil)
	if err != nil {
		return RemoteControlActionResult{}, fmt.Errorf("panelDelete: %w", err)
	}
	session := e.controlPanel
	e.controlPanel = remoteControlSession{}
	ctx := e.lifecycleCtx
	if ctx == nil {
		ctx = context.TODO()
	}
	err = e.deleteCachedRemoteProfile(ctx, session.address, session.identity)
	if err != nil {
		e.log("Remote account deleted, but the launcher cache could not be removed")
	}
	return RemoteControlActionResult{IsCacheUpdated: err == nil}, nil
}

func (e *App) remoteControlActionLocked(serverAddress, identity, method, path string, req, result any) error {
	address, err := normalizeRemoteServerAddress(serverAddress)
	if err != nil {
		return fmt.Errorf("panelAddress: %w", err)
	}
	if e.controlPanel.token == "" || e.controlPanel.address != address || e.controlPanel.identity != strings.TrimSpace(identity) {
		return errors.New("panel session does not match the selected account")
	}
	if e.controlPanel.isLocal && (path == "/account" || path == "/account/password" ||
		path == "/account/sessions/revoke" || path == "/account/sessions/revoke-others") {
		return errors.New("controlPanel:forbidden")
	}
	ctx := e.lifecycleCtx
	if ctx == nil {
		ctx = context.TODO()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err = remoteControlRequest(ctx, address, e.controlPanel.token, method, path, req, result)
	if err != nil {
		if strings.Contains(err.Error(), "controlPanel:unauthorized") || strings.Contains(err.Error(), "controlPanel:forbidden") {
			e.controlPanel = remoteControlSession{}
		}
		return fmt.Errorf("panelCommand: %w", err)
	}
	return nil
}

func (e *App) updateRemoteControlCacheLocked(displayName string, isPasswordCleared bool) error {
	ctx := e.lifecycleCtx
	if ctx == nil {
		ctx = context.TODO()
	}
	database, err := e.openRemoteDatabase(ctx)
	if err != nil {
		return fmt.Errorf("cacheOpen: %w", err)
	}
	defer database.Close()
	var cacheResult sql.Result
	if isPasswordCleared {
		cacheResult, err = database.ExecContext(ctx, `UPDATE remote_profile SET password = '' WHERE server_address = ? AND login_name = ?`, e.controlPanel.address, e.controlPanel.identity)
	} else {
		cacheResult, err = database.ExecContext(ctx, `UPDATE remote_profile SET display_name = ? WHERE server_address = ? AND login_name = ?`, displayName, e.controlPanel.address, e.controlPanel.identity)
	}
	if err != nil {
		return fmt.Errorf("cacheUpdate: %w", err)
	}
	updatedCount, err := cacheResult.RowsAffected()
	if err != nil {
		return fmt.Errorf("cacheCount: %w", err)
	}
	if updatedCount != 1 {
		return errors.New("cached remote profile was not found")
	}
	return nil
}
