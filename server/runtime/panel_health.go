package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/controlpanel"
	"github.com/darkspinnet/darkspin/server/sporenet/savebuffer"
)

type profileConnectionChecker interface {
	CheckConnection(context.Context) error
}

type panelHealth struct {
	connectionChecker profileConnectionChecker
	profileBuffer     *savebuffer.Repository
	runtimePath       string
	configPath        string
	isManaged         bool
	backupMu          sync.Mutex
	backupHealth      controlpanel.BackupHealth
	backupExpiresAt   time.Time
}

func (e *panelHealth) Health(ctx context.Context) (controlpanel.Health, error) {
	err := ctx.Err()
	if err != nil {
		return controlpanel.Health{}, fmt.Errorf("healthContext: %w", err)
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	err = e.connectionChecker.CheckConnection(probeCtx)
	cancel()
	observation := controlpanel.Health{
		Status: "unknown", DatabaseStatus: "healthy", DatabaseObservedAt: time.Now().UTC(),
	}
	if err != nil {
		observation.DatabaseStatus = "degraded"
		observation.Status = "degraded"
	}
	parentErr := ctx.Err()
	if parentErr != nil {
		return controlpanel.Health{}, fmt.Errorf("healthDeadline: %w", parentErr)
	}
	saveHealth := e.profileBuffer.Health()
	observation.ProfileSaves = controlpanel.ProfileSaveHealth{
		LastSuccessAt: saveHealth.LastSuccessAt, LastFailureAt: saveHealth.LastFailureAt,
		PendingAccounts: saveHealth.PendingAccountCount, FailedAccounts: saveHealth.FailedAccountCount,
	}
	if saveHealth.FailedAccountCount > 0 {
		observation.Status = "degraded"
	}
	observation.Storage = observeStorage(e.runtimePath)
	observation.Backup = e.observeBackup()
	if observation.Backup.Status == "failed" || observation.Backup.ArchiveStatus == "missing" ||
		observation.Backup.Recovery.Status == "failed" ||
		observation.Backup.IntegrityStatus == "mismatch" || observation.Backup.Storage.Status == "degraded" {
		observation.Status = "degraded"
	}
	if observation.Storage.Status == "degraded" {
		observation.Status = "degraded"
	}
	if observation.Status != "degraded" && !saveHealth.LastSuccessAt.IsZero() &&
		time.Since(saveHealth.LastSuccessAt) <= 5*time.Minute {
		observation.Status = "healthy"
	}
	observation.ObservedAt = time.Now().UTC()
	return observation, nil
}

func observeStorage(runtimePath string) controlpanel.StorageHealth {
	observation := controlpanel.StorageHealth{Status: "unknown", ObservedAt: time.Now().UTC()}
	if runtimePath == "" {
		return observation
	}
	totalBytes, availableBytes, isKnown := storageCapacity(runtimePath)
	observation.ObservedAt = time.Now().UTC()
	if !isKnown || totalBytes == 0 || availableBytes > totalBytes {
		return observation
	}
	observation.Status = "available"
	observation.TotalBytes = totalBytes
	observation.AvailableBytes = availableBytes
	if availableBytes == 0 {
		observation.Status = "degraded"
	}
	return observation
}
