package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type RemoteControlMaintenance struct {
	ID                   string                          `json:"id"`
	Action               string                          `json:"action"`
	Reason               string                          `json:"reason"`
	Phase                string                          `json:"phase"`
	Deadline             string                          `json:"deadline"`
	IsActive             bool                            `json:"is_active"`
	IsAdmissionBlocked   bool                            `json:"is_admission_blocked"`
	Delivered            int                             `json:"delivered"`
	IsRestartSupported   bool                            `json:"is_restart_supported"`
	IsUpdateSupported    bool                            `json:"is_update_supported"`
	RequestedOperationID string                          `json:"requested_operation_id"`
	Result               *RemoteControlMaintenanceResult `json:"result,omitempty"`
}
type RemoteControlMaintenanceResult struct {
	OperationID      string `json:"operation_id"`
	Action           string `json:"action"`
	Phase            string `json:"phase"`
	UpdatedAt        string `json:"updated_at"`
	InstalledVersion string `json:"installed_version"`
	CandidateVersion string `json:"candidate_version"`
	IsSuccess        *bool  `json:"is_success"`
	FailureCode      string `json:"failure_code"`
}
type RemoteControlMaintenanceRequest struct {
	Action         string `json:"action"`
	WarningMinutes int    `json:"warning_minutes"`
	Reason         string `json:"reason"`
}
type remoteControlOperation struct{ address, identity, id string }

func (e *App) GetRemoteControlMaintenance(serverAddress, identity string) (RemoteControlMaintenance, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlMaintenance{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodGet, "/admin/maintenance", nil, &result)
	if err != nil {
		return RemoteControlMaintenance{}, fmt.Errorf("maintenanceRead: %w", err)
	}
	e.decorateRemoteMaintenanceLocked(&result)
	return result, nil
}

func (e *App) StartRemoteControlMaintenance(serverAddress, identity string, req RemoteControlMaintenanceRequest) (RemoteControlMaintenance, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	if req.Action != "hold" && req.Action != "restart" && req.Action != "update" {
		return RemoteControlMaintenance{}, errors.New("invalid maintenance action")
	}
	result := RemoteControlMaintenance{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPost, "/admin/maintenance/mode", req, &result)
	if err != nil {
		return RemoteControlMaintenance{}, fmt.Errorf("maintenanceStart: %w", err)
	}
	if result.ID != "" {
		e.controlOperation = remoteControlOperation{address: e.controlPanel.address, identity: e.controlPanel.identity, id: result.ID}
	}
	e.decorateRemoteMaintenanceLocked(&result)
	return result, nil
}

func (e *App) CancelRemoteControlMaintenance(serverAddress, identity, operationID string) (RemoteControlMaintenance, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	if strings.TrimSpace(operationID) == "" {
		return RemoteControlMaintenance{}, errors.New("maintenance operation ID is required")
	}
	result := RemoteControlMaintenance{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPost, "/admin/maintenance/cancel", map[string]string{"id": operationID}, &result)
	if err != nil {
		return RemoteControlMaintenance{}, fmt.Errorf("maintenanceCancel: %w", err)
	}
	e.decorateRemoteMaintenanceLocked(&result)
	return result, nil
}

func (e *App) decorateRemoteMaintenanceLocked(result *RemoteControlMaintenance) {
	if result.Result != nil && result.Result.OperationID != result.ID {
		result.Result = nil
	}
	if result.Result != nil {
		switch result.Result.FailureCode {
		case "", "update_failed", "startup_failed", "rollback_failed":
		default:
			result.Result.FailureCode = "unknown"
		}
	}
	if e.controlOperation.address != e.controlPanel.address || e.controlOperation.identity != e.controlPanel.identity {
		return
	}
	// Retain only correlation, never a cached outcome, across panel sign-ins.
	result.RequestedOperationID = e.controlOperation.id
	if result.ID != e.controlOperation.id || result.IsActive {
		return
	}
	if (result.Action == "restart" || result.Action == "update") && (result.Result == nil || result.Result.IsSuccess == nil) && result.Phase != "cancelled" {
		return
	}
	switch result.Phase {
	case "completed", "healthy", "rolled_back", "failed", "cancelled":
		e.controlOperation = remoteControlOperation{}
	}
}
