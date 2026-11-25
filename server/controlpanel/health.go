package controlpanel

import (
	"context"
	"net/http"
	"time"
)

type ProfileSaveHealth struct {
	LastSuccessAt   time.Time
	LastFailureAt   time.Time
	PendingAccounts int
	FailedAccounts  int
}

// StorageHealth observes a server-owned filesystem's capacity, not successful writes.
type StorageHealth struct {
	Status         string
	ObservedAt     time.Time
	TotalBytes     uint64
	AvailableBytes uint64
}

type BackupSuccessHealth struct {
	ID          string
	StartedAt   time.Time
	CompletedAt time.Time
	BuildID     string
	AgeSeconds  uint64
}

// RestoreHealth describes one recorded recovery, not every retained archive.
type RestoreHealth struct {
	Status      string
	BackupID    string
	StartedAt   time.Time
	UpdatedAt   time.Time
	RestoredAt  time.Time
	HealthyAt   time.Time
	FailureCode string
}

type BackupHealth struct {
	Recovery          RestoreHealth
	Status            string
	ObservedAt        time.Time
	LastAttemptAt     time.Time
	LastFailureAt     time.Time
	FailureCode       string
	LastSuccess       *BackupSuccessHealth
	Storage           StorageHealth
	ArchiveStatus     string
	ArchiveObservedAt time.Time
	IntegrityStatus   string
	RestoreStatus     string
}

type Health struct {
	ObservedAt         time.Time
	Status             string
	DatabaseStatus     string
	DatabaseObservedAt time.Time
	ProfileSaves       ProfileSaveHealth
	Storage            StorageHealth
	Backup             BackupHealth
}

type HealthReader interface {
	Health(context.Context) (Health, error)
}

type healthObservationDTO struct {
	Status     string    `json:"status"`
	ObservedAt time.Time `json:"observed_at,omitempty"`
}

type profileSaveHealthDTO struct {
	LastSuccessAt    time.Time `json:"last_success_at"`
	LastFailureAt    time.Time `json:"last_failure_at"`
	PendingAccounts  int       `json:"pending_accounts"`
	FailedAccounts   int       `json:"failed_accounts"`
	IsPendingFailure bool      `json:"is_pending_failure"`
}

type storageHealthDTO struct {
	Status         string    `json:"status"`
	ObservedAt     time.Time `json:"observed_at"`
	TotalBytes     *uint64   `json:"total_bytes,omitempty"`
	AvailableBytes *uint64   `json:"available_bytes,omitempty"`
}

type backupSuccessDTO struct {
	ID          string    `json:"id"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	BuildID     string    `json:"build_id"`
	AgeSeconds  uint64    `json:"age_seconds"`
}

type backupHealthDTO struct {
	Recovery          restoreHealthDTO  `json:"recovery"`
	Status            string            `json:"status"`
	ObservedAt        time.Time         `json:"observed_at"`
	LastAttemptAt     time.Time         `json:"last_attempt_at"`
	LastFailureAt     time.Time         `json:"last_failure_at"`
	FailureCode       string            `json:"failure_code"`
	LastSuccess       *backupSuccessDTO `json:"last_success,omitempty"`
	Storage           storageHealthDTO  `json:"storage"`
	ArchiveStatus     string            `json:"archive_status"`
	ArchiveObservedAt time.Time         `json:"archive_observed_at"`
	IntegrityStatus   string            `json:"integrity_status"`
	RestoreStatus     string            `json:"restore_status"`
}

type restoreHealthDTO struct {
	Status      string    `json:"status"`
	BackupID    string    `json:"backup_id"`
	StartedAt   time.Time `json:"started_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	RestoredAt  time.Time `json:"restored_at"`
	HealthyAt   time.Time `json:"healthy_at"`
	FailureCode string    `json:"failure_code"`
}

type healthDTO struct {
	ObservedAt   time.Time            `json:"observed_at"`
	Status       string               `json:"status"`
	Database     healthObservationDTO `json:"database"`
	ProfileSaves profileSaveHealthDTO `json:"profile_saves"`
	Storage      storageHealthDTO     `json:"storage"`
	Backup       backupHealthDTO      `json:"backup"`
}

func (e *Service) health(writer http.ResponseWriter, req *http.Request) {
	health, err := e.config.HealthReader.Health(req.Context())
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "Health information is unavailable.")
		return
	}
	storage := storageDTO(health.Storage)
	backup := backupHealthDTO{Status: health.Backup.Status, ObservedAt: health.Backup.ObservedAt,
		Recovery:      restoreDTO(health.Backup.Recovery),
		LastAttemptAt: health.Backup.LastAttemptAt, LastFailureAt: health.Backup.LastFailureAt,
		FailureCode: health.Backup.FailureCode, ArchiveStatus: health.Backup.ArchiveStatus,
		ArchiveObservedAt: health.Backup.ArchiveObservedAt, IntegrityStatus: health.Backup.IntegrityStatus,
		RestoreStatus: health.Backup.RestoreStatus, Storage: storageDTO(health.Backup.Storage)}
	if backup.Status != "completed" && backup.Status != "failed" {
		backup.Status = "unknown"
	}
	if health.Backup.LastSuccess != nil {
		success := health.Backup.LastSuccess
		backup.LastSuccess = &backupSuccessDTO{ID: success.ID, StartedAt: success.StartedAt,
			CompletedAt: success.CompletedAt, BuildID: success.BuildID, AgeSeconds: success.AgeSeconds}
	}
	writeJSON(writer, http.StatusOK, healthDTO{
		ObservedAt: health.ObservedAt, Status: health.Status,
		Database: healthObservationDTO{Status: health.DatabaseStatus, ObservedAt: health.DatabaseObservedAt},
		ProfileSaves: profileSaveHealthDTO{LastSuccessAt: health.ProfileSaves.LastSuccessAt, LastFailureAt: health.ProfileSaves.LastFailureAt,
			PendingAccounts: health.ProfileSaves.PendingAccounts, FailedAccounts: health.ProfileSaves.FailedAccounts,
			IsPendingFailure: health.ProfileSaves.FailedAccounts > 0},
		Storage: storage,
		Backup:  backup,
	})
}

func restoreDTO(observation RestoreHealth) restoreHealthDTO {
	status := observation.Status
	if status != "restored" && status != "healthy" && status != "failed" {
		status = "unknown"
	}
	return restoreHealthDTO{Status: status, BackupID: observation.BackupID,
		StartedAt: observation.StartedAt.UTC(), UpdatedAt: observation.UpdatedAt.UTC(),
		RestoredAt: observation.RestoredAt.UTC(), HealthyAt: observation.HealthyAt.UTC(), FailureCode: observation.FailureCode}
}

func storageDTO(observation StorageHealth) storageHealthDTO {
	result := storageHealthDTO{Status: observation.Status, ObservedAt: observation.ObservedAt}
	if result.Status != "available" && result.Status != "degraded" {
		result.Status = "unknown"
		return result
	}
	result.TotalBytes = &observation.TotalBytes
	result.AvailableBytes = &observation.AvailableBytes
	return result
}
