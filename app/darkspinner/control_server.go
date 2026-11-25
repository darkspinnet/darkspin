package main

import (
	"fmt"
	"net/http"
)

type RemoteControlAnnouncement struct {
	ID                string `json:"id"`
	Message           string `json:"message"`
	CreatedAt         string `json:"created_at"`
	ExpiresAt         string `json:"expires_at"`
	Delivered         int    `json:"delivered"`
	IsDeliveryPartial bool   `json:"is_delivery_partial"`
	IsAuditRecorded   bool   `json:"is_audit_recorded"`
	Outcome           string `json:"outcome"`
}
type RemoteControlAnnouncementList struct {
	Announcements []RemoteControlAnnouncement `json:"announcements"`
}
type remoteControlObservation struct {
	Status     string `json:"status"`
	ObservedAt string `json:"observed_at"`
}
type remoteControlProfileSaveHealth struct {
	LastSuccessAt    string `json:"last_success_at"`
	LastFailureAt    string `json:"last_failure_at"`
	PendingAccounts  int    `json:"pending_accounts"`
	FailedAccounts   int    `json:"failed_accounts"`
	IsPendingFailure bool   `json:"is_pending_failure"`
}
type remoteControlStorageHealth struct {
	Status         string  `json:"status"`
	ObservedAt     string  `json:"observed_at"`
	TotalBytes     *uint64 `json:"total_bytes,omitempty"`
	AvailableBytes *uint64 `json:"available_bytes,omitempty"`
}

type remoteControlBackupSuccess struct {
	ID          string `json:"id"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
	BuildID     string `json:"build_id"`
	AgeSeconds  uint64 `json:"age_seconds"`
}

type remoteControlBackupHealth struct {
	Recovery          remoteControlRestoreHealth  `json:"recovery"`
	Status            string                      `json:"status"`
	ObservedAt        string                      `json:"observed_at"`
	LastAttemptAt     string                      `json:"last_attempt_at"`
	LastFailureAt     string                      `json:"last_failure_at"`
	FailureCode       string                      `json:"failure_code"`
	LastSuccess       *remoteControlBackupSuccess `json:"last_success,omitempty"`
	Storage           remoteControlStorageHealth  `json:"storage"`
	ArchiveStatus     string                      `json:"archive_status"`
	ArchiveObservedAt string                      `json:"archive_observed_at"`
	IntegrityStatus   string                      `json:"integrity_status"`
	RestoreStatus     string                      `json:"restore_status"`
}

type remoteControlRestoreHealth struct {
	Status      string `json:"status"`
	BackupID    string `json:"backup_id"`
	StartedAt   string `json:"started_at"`
	UpdatedAt   string `json:"updated_at"`
	RestoredAt  string `json:"restored_at"`
	HealthyAt   string `json:"healthy_at"`
	FailureCode string `json:"failure_code"`
}
type RemoteControlHealth struct {
	ObservedAt   string                         `json:"observed_at"`
	Status       string                         `json:"status"`
	Database     remoteControlObservation       `json:"database"`
	ProfileSaves remoteControlProfileSaveHealth `json:"profile_saves"`
	Storage      remoteControlStorageHealth     `json:"storage"`
	Backup       remoteControlBackupHealth      `json:"backup"`
}

func (e *App) GetRemoteControlIdentity(serverAddress, identity string) (ServerIdentityConfiguration, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := ServerIdentityConfiguration{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodGet, "/server/identity", nil, &result)
	if err != nil {
		return ServerIdentityConfiguration{}, fmt.Errorf("identityRead: %w", err)
	}
	return result, nil
}
func (e *App) SetRemoteControlIdentity(serverAddress, identity string, req ServerIdentityConfiguration) (ServerIdentityConfiguration, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := ServerIdentityConfiguration{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPatch, "/admin/identity", req, &result)
	if err != nil {
		return ServerIdentityConfiguration{}, fmt.Errorf("identitySave: %w", err)
	}
	return result, nil
}
func (e *App) GetRemoteControlAnnouncements(serverAddress, identity string) (RemoteControlAnnouncementList, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlAnnouncementList{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodGet, "/server/announcements", nil, &result)
	if err != nil {
		return RemoteControlAnnouncementList{}, fmt.Errorf("announcementRead: %w", err)
	}
	return result, nil
}
func (e *App) SendRemoteControlAnnouncement(serverAddress, identity, message string) (RemoteControlAnnouncement, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlAnnouncement{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPost, "/admin/announcements", map[string]string{"message": message}, &result)
	if err != nil {
		return RemoteControlAnnouncement{}, fmt.Errorf("announcementSend: %w", err)
	}
	return result, nil
}
func (e *App) GetRemoteControlHealth(serverAddress, identity string) (RemoteControlHealth, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlHealth{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodGet, "/admin/health", nil, &result)
	if err != nil {
		return RemoteControlHealth{}, fmt.Errorf("healthRead: %w", err)
	}
	return result, nil
}
