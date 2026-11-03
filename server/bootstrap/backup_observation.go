package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// BackupSuccess observes a completed snapshot manifest. It does not certify
// that its archive still exists or that a restore has been verified.
type BackupSuccess struct {
	ID          string    `json:"id"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	BuildID     string    `json:"build_id"`
}

// BackupObservation contains public-safe supervisor observations only.
// An attempt newer than either terminal observation has an unknown outcome.
type BackupObservation struct {
	UpdatedAt     time.Time      `json:"updated_at"`
	LastAttemptAt time.Time      `json:"last_attempt_at"`
	LastFailureAt time.Time      `json:"last_failure_at,omitempty"`
	FailureCode   string         `json:"failure_code,omitempty"`
	LastSuccess   *BackupSuccess `json:"last_success,omitempty"`
}

var backupObservationIDPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}/\d{2}-\d{2}-\d{2}\.\d{9}-[a-f0-9]{12}$`)
var backupObservationBuildPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,255}$`)

// ReadBackupObservation reads one bounded metadata record from the directory
// containing darkspin.toml. It never inspects a snapshot or database dump.
// Absence preserves errors.Is(err, os.ErrNotExist).
func ReadBackupObservation(runtimeDirectory string) (BackupObservation, error) {
	var observation BackupObservation
	r, err := os.Open(filepath.Join(runtimeDirectory, "backup-observation.json"))
	if err != nil {
		return observation, fmt.Errorf("observationOpen: %w", err)
	}
	payload, err := io.ReadAll(io.LimitReader(r, 16385))
	closeErr := r.Close()
	if err != nil || closeErr != nil {
		return observation, fmt.Errorf("observationRead: %w", errors.Join(err, closeErr))
	}
	if len(payload) > 16384 {
		return observation, errors.New("backup observation exceeds size limit")
	}
	err = json.Unmarshal(payload, &observation)
	if err != nil {
		return BackupObservation{}, fmt.Errorf("observationDecode: %w", err)
	}
	err = validateBackupObservation(observation)
	if err != nil {
		return BackupObservation{}, fmt.Errorf("observationValidate: %w", err)
	}
	return observation, nil
}

func validateBackupObservation(observation BackupObservation) error {
	if observation.UpdatedAt.IsZero() || observation.LastAttemptAt.IsZero() ||
		observation.LastAttemptAt.After(observation.UpdatedAt) ||
		observation.LastFailureAt.After(observation.UpdatedAt) {
		return errors.New("invalid backup observation time")
	}
	if (observation.LastFailureAt.IsZero() && observation.FailureCode != "") ||
		(!observation.LastFailureAt.IsZero() && observation.FailureCode != "backup_failed") {
		return errors.New("invalid backup observation failure")
	}
	if observation.LastSuccess == nil {
		return nil
	}
	success := observation.LastSuccess
	if !backupObservationIDPattern.MatchString(success.ID) ||
		!backupObservationBuildPattern.MatchString(success.BuildID) ||
		success.StartedAt.IsZero() || success.CompletedAt.Before(success.StartedAt) ||
		success.StartedAt.After(observation.LastAttemptAt) ||
		success.CompletedAt.After(observation.UpdatedAt) {
		return errors.New("invalid backup success observation")
	}
	snapshotAt, err := time.Parse("2006-01-02/15-04-05.000000000", success.ID[:29])
	if err != nil {
		return fmt.Errorf("observationID: %w", err)
	}
	if snapshotAt.Format("2006-01-02/15-04-05.000000000") != success.ID[:29] {
		return errors.New("invalid backup observation identity")
	}
	return nil
}

func (e *Storage) writeBackupObservation(observation BackupObservation) error {
	err := validateBackupObservation(observation)
	if err != nil {
		return fmt.Errorf("backupValidate: %w", err)
	}
	err = e.writeJSON(e.path("runtime", "backup-observation.json"), observation)
	if err != nil {
		return fmt.Errorf("backupWrite: %w", err)
	}
	return nil
}

// observedSnapshot keeps observation failures separate from snapshot success
// and the updater transaction. Only SnapshotContext establishes completion.
func (e *supervisor) observedSnapshot(ctx context.Context, buildID string) (string, error) {
	observation, readErr := ReadBackupObservation(e.storage.path("runtime"))
	if readErr != nil {
		observation = BackupObservation{}
		if !errors.Is(readErr, os.ErrNotExist) {
			e.option.Logger.Error("read backup observation", "error", readErr)
		}
	}
	startedAt := time.Now().UTC()
	observation.LastAttemptAt = startedAt
	observation.UpdatedAt = startedAt
	startErr := e.storage.writeBackupObservation(observation)
	if startErr != nil {
		e.option.Logger.Error("record backup attempt", "error", startErr)
	}
	id, err := e.storage.SnapshotContext(ctx, buildID)
	if startErr == nil {
		observation.UpdatedAt = time.Now().UTC()
		if err != nil {
			observation.LastFailureAt = observation.UpdatedAt
			observation.FailureCode = "backup_failed"
		} else {
			observation.LastSuccess = &BackupSuccess{
				ID: id, StartedAt: startedAt, CompletedAt: observation.UpdatedAt, BuildID: buildID,
			}
		}
		writeErr := e.storage.writeBackupObservation(observation)
		if writeErr != nil {
			e.option.Logger.Error("record backup outcome", "error", writeErr)
		}
	}
	if err != nil {
		return "", fmt.Errorf("observedSnapshot: %w", err)
	}
	return id, nil
}
