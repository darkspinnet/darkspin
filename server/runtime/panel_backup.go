package runtime

import (
	"os"
	"path/filepath"
	"time"

	"github.com/darkspinnet/darkspin/server/bootstrap"
	"github.com/darkspinnet/darkspin/server/controlpanel"
	"github.com/darkspinnet/darkspin/server/game"
)

func (e *panelHealth) observeBackup() controlpanel.BackupHealth {
	e.backupMu.Lock()
	defer e.backupMu.Unlock()
	now := time.Now().UTC()
	if now.Before(e.backupExpiresAt) {
		return copyBackupHealth(e.backupHealth, now)
	}
	e.backupHealth = observeBackup(e.configPath, e.isManaged)
	e.backupExpiresAt = now.Add(30 * time.Second)
	return copyBackupHealth(e.backupHealth, now)
}

func copyBackupHealth(observation controlpanel.BackupHealth, now time.Time) controlpanel.BackupHealth {
	if observation.LastSuccess != nil {
		success := *observation.LastSuccess
		age := now.Sub(success.CompletedAt)
		if age >= 0 {
			success.AgeSeconds = uint64(age / time.Second)
		}
		observation.LastSuccess = &success
	}
	return observation
}

func observeBackup(configPath string, isManaged bool) controlpanel.BackupHealth {
	observation := controlpanel.BackupHealth{Status: "unknown", ObservedAt: time.Now().UTC(),
		Recovery:      controlpanel.RestoreHealth{Status: "unknown"},
		ArchiveStatus: "unknown", IntegrityStatus: "unknown", RestoreStatus: "not_verified",
		Storage: controlpanel.StorageHealth{Status: "unknown", ObservedAt: time.Now().UTC()}}
	rootPath := os.Getenv("DARKSPIN_BOOTSTRAP_ROOT")
	if !isManaged || rootPath == "" || !filepath.IsAbs(rootPath) ||
		filepath.Clean(configPath) != filepath.Join(rootPath, "runtime", game.DefaultConfigFilename) {
		return observation
	}
	backupDirectory, directoryErr := bootstrap.BackupDirectory(rootPath)
	observation.Recovery = observeRestore(filepath.Dir(configPath))
	if directoryErr == nil {
		observation.Storage = observeStorage(backupDirectory)
	}
	record, err := bootstrap.ReadBackupObservation(filepath.Dir(configPath))
	if err != nil {
		// Missing, unreadable, or invalid records cannot establish backup state.
		return observation
	}
	observation.ObservedAt = record.UpdatedAt
	observation.LastAttemptAt = record.LastAttemptAt
	observation.LastFailureAt = record.LastFailureAt
	observation.FailureCode = record.FailureCode
	terminalAt := record.LastFailureAt
	if record.LastSuccess != nil {
		success := record.LastSuccess
		age := time.Since(success.CompletedAt)
		if age < 0 {
			return observation
		}
		observation.LastSuccess = &controlpanel.BackupSuccessHealth{ID: success.ID,
			StartedAt: success.StartedAt, CompletedAt: success.CompletedAt, BuildID: success.BuildID,
			AgeSeconds: uint64(age / time.Second)}
		archive := bootstrap.ObserveBackupArchive(rootPath, *success)
		observation.ArchiveStatus = archive.Status
		observation.ArchiveObservedAt = time.Now().UTC()
		observation.IntegrityStatus = archive.IntegrityStatus
		observation.RestoreStatus = archive.RestoreStatus
		if observation.Recovery.Status == "healthy" && observation.Recovery.BackupID == success.ID {
			observation.RestoreStatus = "recovered_healthy"
		}
		if success.CompletedAt.After(terminalAt) {
			terminalAt = success.CompletedAt
			observation.Status = "completed"
		}
	}
	if !record.LastFailureAt.IsZero() && !record.LastFailureAt.Before(terminalAt) {
		observation.Status = "failed"
	}
	if record.LastAttemptAt.After(terminalAt) {
		// An unfinished recorded attempt may have been interrupted by a crash.
		observation.Status = "unknown"
	}
	return observation
}
