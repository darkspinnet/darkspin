package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// RestoreObservation describes the latest actual rollback, rather than an
// archive inspection. Healthy certifies its first restored startup only.
type RestoreObservation struct {
	BackupID    string    `json:"backup_id"`
	StartedAt   time.Time `json:"started_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	RestoredAt  time.Time `json:"restored_at,omitempty"`
	HealthyAt   time.Time `json:"healthy_at,omitempty"`
	Status      string    `json:"status"`
	FailureCode string    `json:"failure_code,omitempty"`
}

type restoreRecord struct {
	RestoreObservation
	BinaryHash string    `json:"binary_sha256,omitempty"`
	ClaimedAt  time.Time `json:"claimed_at,omitempty"`
}

type restoreAttempt struct {
	record     restoreRecord
	isRecorded bool
}

var restoreHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// ReadRestoreObservation reads at most 16 KiB of allowlisted metadata. An empty
// BackupID is allowed only for failures before a valid transaction was read.
// Absence preserves errors.Is(err, os.ErrNotExist).
func ReadRestoreObservation(runtimeDirectory string) (RestoreObservation, error) {
	record, err := readRestoreRecord(runtimeDirectory)
	if err != nil {
		return RestoreObservation{}, fmt.Errorf("restoreRead: %w", err)
	}
	return record.RestoreObservation, nil
}

func readRestoreRecord(runtimeDirectory string) (restoreRecord, error) {
	r, err := os.Open(filepath.Join(runtimeDirectory, "restore-observation.json"))
	if err != nil {
		return restoreRecord{}, fmt.Errorf("restoreOpen: %w", err)
	}
	payload, err := io.ReadAll(io.LimitReader(r, 16385))
	closeErr := r.Close()
	if err != nil || closeErr != nil {
		return restoreRecord{}, fmt.Errorf("restorePayload: %w", errors.Join(err, closeErr))
	}
	if len(payload) > 16384 {
		return restoreRecord{}, errors.New("restore observation exceeds size limit")
	}
	var record restoreRecord
	err = json.Unmarshal(payload, &record)
	if err != nil {
		return restoreRecord{}, fmt.Errorf("restoreDecode: %w", err)
	}
	err = validateRestoreRecord(record)
	if err != nil {
		return restoreRecord{}, fmt.Errorf("restoreValidate: %w", err)
	}
	return record, nil
}

func validateRestoreRecord(record restoreRecord) error {
	if record.StartedAt.IsZero() || record.UpdatedAt.Before(record.StartedAt) ||
		record.RestoredAt.After(record.UpdatedAt) || record.HealthyAt.After(record.UpdatedAt) ||
		record.ClaimedAt.After(record.UpdatedAt) {
		return errors.New("invalid restore observation chronology")
	}
	if record.BackupID == "" {
		if record.Status != "failed" {
			return errors.New("missing restore backup identity")
		}
	} else {
		if !backupObservationIDPattern.MatchString(record.BackupID) {
			return errors.New("invalid restore backup identity")
		}
		snapshotAt, err := time.Parse("2006-01-02/15-04-05.000000000", record.BackupID[:29])
		if err != nil {
			return fmt.Errorf("restoreIdentity: %w", err)
		}
		if snapshotAt.Format("2006-01-02/15-04-05.000000000") != record.BackupID[:29] {
			return errors.New("invalid restore backup date")
		}
	}
	if record.BinaryHash != "" && !restoreHashPattern.MatchString(record.BinaryHash) {
		return errors.New("invalid restored binary identity")
	}
	switch record.Status {
	case "restoring":
		if !record.RestoredAt.IsZero() || !record.HealthyAt.IsZero() || !record.ClaimedAt.IsZero() || record.FailureCode != "" {
			return errors.New("invalid restoring observation")
		}
	case "failed":
		if !record.RestoredAt.IsZero() || !record.HealthyAt.IsZero() || !record.ClaimedAt.IsZero() || record.FailureCode != "restore_failed" {
			return errors.New("invalid failed restore observation")
		}
	case "restored", "healthy":
		if record.RestoredAt.IsZero() || record.RestoredAt.Before(record.StartedAt) || record.FailureCode != "" ||
			!restoreHashPattern.MatchString(record.BinaryHash) ||
			(!record.ClaimedAt.IsZero() && record.ClaimedAt.Before(record.RestoredAt)) {
			return errors.New("invalid completed restore observation")
		}
		if record.Status == "restored" && !record.HealthyAt.IsZero() {
			return errors.New("restored observation has health result")
		}
		if record.Status == "healthy" && (record.ClaimedAt.IsZero() || record.HealthyAt.IsZero() || record.HealthyAt.Before(record.ClaimedAt)) {
			return errors.New("invalid restore health chronology")
		}
	default:
		return errors.New("invalid restore observation status")
	}
	return nil
}

func (e *Storage) writeRestoreRecord(record restoreRecord) error {
	err := validateRestoreRecord(record)
	if err != nil {
		return fmt.Errorf("restoreRecord: %w", err)
	}
	err = e.writeJSON(e.path("runtime", "restore-observation.json"), record)
	if err != nil {
		return fmt.Errorf("restoreWrite: %w", err)
	}
	return nil
}

func (e *Storage) logRestoreObservation(err error) {
	logger := e.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error("record restore observation", "error", err)
}

func (e *Storage) startRestoreObservation(attempt *restoreAttempt, backupID string) {
	e.restoreStartedAt = time.Time{}
	attempt.record.BackupID = filepath.ToSlash(backupID)
	attempt.record.UpdatedAt = attempt.record.StartedAt
	err := e.writeRestoreRecord(attempt.record)
	if err != nil {
		validationErr := validateRestoreRecord(attempt.record)
		if validationErr != nil {
			attempt.record.BackupID = ""
		}
		e.logRestoreObservation(err)
		return
	}
	attempt.isRecorded = true
}

func (e *Storage) finishRestoreObservation(attempt *restoreAttempt, rollbackErr *error) {
	if *rollbackErr == nil && !attempt.isRecorded && attempt.record.BackupID == "" {
		return
	}
	record := attempt.record
	record.UpdatedAt = time.Now().UTC()
	if *rollbackErr != nil {
		record.Status = "failed"
		record.FailureCode = "restore_failed"
	} else if attempt.isRecorded {
		record.Status = "restored"
		record.RestoredAt = record.UpdatedAt
	} else {
		// Retry recording the attempt without certifying its completion when
		// its initial durable write failed. A reader sees an unknown outcome.
		record.BinaryHash = ""
	}
	err := e.writeRestoreRecord(record)
	if err != nil {
		e.logRestoreObservation(err)
		return
	}
	if record.Status == "restored" {
		e.restoreStartedAt = record.StartedAt
	}
}

// claimRestoredStartup consumes the only startup eligible to certify this
// rollback. A failed launch or supervisor restart cannot reuse that claim.
func (e *Storage) claimRestoredStartup(isFirstBoot bool) restoreRecord {
	startedAt := e.restoreStartedAt
	e.restoreStartedAt = time.Time{}
	if startedAt.IsZero() {
		return restoreRecord{}
	}
	record, err := readRestoreRecord(e.path("runtime"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			e.logRestoreObservation(err)
		}
		return restoreRecord{}
	}
	if record.Status != "restored" || !record.StartedAt.Equal(startedAt) || !record.ClaimedAt.IsZero() {
		return restoreRecord{}
	}
	record.ClaimedAt = time.Now().UTC()
	record.UpdatedAt = record.ClaimedAt
	err = e.writeRestoreRecord(record)
	if err != nil {
		e.logRestoreObservation(err)
		return restoreRecord{}
	}
	// Claim even a mismatching or candidate startup, preventing a later retry
	// from retrospectively certifying a different launch.
	if isFirstBoot {
		return restoreRecord{}
	}
	binaryHash, err := hashFile(e.path("bin", "darkrun"))
	if err != nil {
		e.logRestoreObservation(err)
		return restoreRecord{}
	}
	if binaryHash != record.BinaryHash {
		return restoreRecord{}
	}
	return record
}

func (e *Storage) confirmRestoredStartup(claim restoreRecord) {
	if claim.Status != "restored" {
		return
	}
	record, err := readRestoreRecord(e.path("runtime"))
	if err != nil {
		e.logRestoreObservation(err)
		return
	}
	if record.Status != "restored" || record.BackupID != claim.BackupID ||
		!record.StartedAt.Equal(claim.StartedAt) || !record.ClaimedAt.Equal(claim.ClaimedAt) || record.BinaryHash != claim.BinaryHash {
		return
	}
	binaryHash, err := hashFile(e.path("bin", "darkrun"))
	if err != nil {
		e.logRestoreObservation(err)
		return
	}
	if binaryHash != record.BinaryHash {
		return
	}
	record.Status = "healthy"
	record.HealthyAt = time.Now().UTC()
	record.UpdatedAt = record.HealthyAt
	err = e.writeRestoreRecord(record)
	if err != nil {
		e.logRestoreObservation(err)
	}
}
