package main

import (
	"fmt"
	"net/http"
)

// RemoteControlUpdate is an allowlisted projection of updater observations.
type RemoteControlUpdate struct {
	InstalledVersion  string  `json:"installed_version"`
	InstalledBuildID  string  `json:"installed_build_id"`
	BootstrapVersion  string  `json:"bootstrap_version"`
	BootstrapBuildID  string  `json:"bootstrap_build_id"`
	Channel           string  `json:"channel"`
	CheckStatus       string  `json:"check_status"`
	CheckedAt         *string `json:"checked_at"`
	CandidateVersion  string  `json:"candidate_version"`
	CandidateBuildID  string  `json:"candidate_build_id"`
	FailureCode       string  `json:"failure_code"`
	IsCheckSupported  bool    `json:"is_check_supported"`
	IsCheckInProgress bool    `json:"is_check_in_progress"`
}

func (e *App) GetRemoteControlUpdate(serverAddress, identity string) (RemoteControlUpdate, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlUpdate{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodGet, "/admin/update", nil, &result)
	if err != nil {
		return RemoteControlUpdate{}, fmt.Errorf("updateOverview: %w", err)
	}
	return result, nil
}

func (e *App) CheckRemoteControlUpdate(serverAddress, identity string) (RemoteControlUpdate, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlUpdate{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPost, "/admin/update/check", struct{}{}, &result)
	if err != nil {
		return RemoteControlUpdate{}, fmt.Errorf("updateCheck: %w", err)
	}
	return result, nil
}
