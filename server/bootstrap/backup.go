package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var snapshotPaths = []string{"bin/darkrun", "runtime/darkspin.toml", "runtime/server.toml", "runtime/darkspin/saves"}

func (e *Storage) backupPath(id string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(id))
	if clean == "." || filepath.IsAbs(clean) || strings.Contains(clean, ":") {
		return "", errors.New("invalid backup identity")
	}
	path := e.path("backup", clean)
	err := e.checkPath(path)
	if err != nil {
		return "", fmt.Errorf("backupCheck: %w", err)
	}
	rel, err := filepath.Rel(e.path("backup"), path)
	if err != nil {
		return "", fmt.Errorf("backupRel: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("backup identity escapes backup directory")
	}
	return path, nil
}

// Snapshot captures the executable and all persistent databases after clean stop.
func (e *Storage) Snapshot(buildID string) (string, error) {
	id, err := e.SnapshotContext(context.Background(), buildID)
	if err != nil {
		return "", fmt.Errorf("snapshotContext: %w", err)
	}
	return id, nil
}

func (e *Storage) SnapshotContext(ctx context.Context, buildID string) (string, error) {
	err := e.checkPath(e.path("bin", "darkrun"))
	if err != nil {
		return "", fmt.Errorf("snapshotCheck: %w", err)
	}
	previousHash, err := hashFile(e.path("bin", "darkrun"))
	if err != nil {
		return "", fmt.Errorf("snapshotHash: %w", err)
	}
	now := time.Now().UTC()
	label := previousHash[:12]
	id := filepath.Join(now.Format("2006-01-02"), now.Format("15-04-05.000000000")+"-"+label)
	destination, err := e.backupPath(id)
	if err != nil {
		return "", fmt.Errorf("snapshotPath: %w", err)
	}
	err = os.MkdirAll(destination, 0700)
	if err != nil {
		return "", fmt.Errorf("snapshotDirectory: %w", err)
	}
	for _, rel := range snapshotPaths {
		source := e.path(filepath.FromSlash(rel))
		fi, statErr := os.Lstat(source)
		if errors.Is(statErr, os.ErrNotExist) && rel != "bin/darkrun" {
			continue
		}
		if statErr != nil {
			return "", fmt.Errorf("snapshotStat: %w", statErr)
		}
		target := filepath.Join(destination, filepath.FromSlash(rel))
		if fi.IsDir() {
			err = e.copyTree(source, target)
		} else {
			err = e.copyFile(source, target, fi.Mode().Perm())
		}
		if err != nil {
			return "", fmt.Errorf("snapshotCopy: %w", err)
		}
	}
	manifest := snapshotManifest{Created: now.Format(time.RFC3339Nano), PreviousBuild: buildID, PreviousHash: previousHash, IsComplete: true}
	err = e.snapshotDatabase(ctx, destination, &manifest)
	if err != nil {
		return "", fmt.Errorf("databaseSnapshot: %w", err)
	}
	err = e.writeJSON(filepath.Join(destination, "snapshot.json"), manifest)
	if err != nil {
		return "", fmt.Errorf("snapshotManifest: %w", err)
	}
	return filepath.ToSlash(id), nil
}

// Recover must precede the first process launch after bootstrap starts. A
// transaction always means the candidate has not yet completed its first boot.
func (e *Storage) Recover() (bool, error) {
	isRecovered, err := e.RecoverContext(context.Background())
	if err != nil {
		return false, fmt.Errorf("recoverContext: %w", err)
	}
	return isRecovered, nil
}

func (e *Storage) RecoverContext(ctx context.Context) (bool, error) {
	err := e.checkPath(e.path("firstboot.json"))
	if err != nil {
		return false, fmt.Errorf("recoverCheck: %w", err)
	}
	fi, err := os.Lstat(e.path("firstboot.json"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("recoverStat: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return false, errors.New("firstboot state is not a regular file")
	}
	var transaction updateTransaction
	err = readJSON(e.path("firstboot.json"), &transaction)
	if err != nil {
		return false, fmt.Errorf("recoverState: %w", err)
	}
	if transaction.Phase == "healthy" {
		// A preceding health commit may have renamed the marker but failed its
		// directory sync. Do not accept gameplay progress until that commit is
		// durable; retention failures remain separate, nonfatal housekeeping.
		err = syncDirectory(e.root)
		if err != nil {
			return false, fmt.Errorf("recoverCommit: %w", err)
		}
		return false, nil
	}
	err = e.RollbackContext(ctx)
	if err != nil {
		return false, fmt.Errorf("recoverRollback: %w", err)
	}
	return true, nil
}

// Rollback is repeatable at every crash point. Failed candidate state is retained
// beside its matching snapshot. Restore stages never replace the only backup.
func (e *Storage) Rollback() error {
	err := e.RollbackContext(context.Background())
	if err != nil {
		return fmt.Errorf("rollbackContext: %w", err)
	}
	return nil
}

func (e *Storage) RollbackContext(ctx context.Context) error {
	statePath := e.path("firstboot.json")
	var transaction updateTransaction
	err := e.checkPath(statePath)
	if err != nil {
		return fmt.Errorf("rollbackCheck: %w", err)
	}
	err = readJSON(statePath, &transaction)
	if err != nil {
		return fmt.Errorf("rollbackState: %w", err)
	}
	backup, err := e.backupPath(transaction.Backup)
	if err != nil {
		return fmt.Errorf("rollbackPath: %w", err)
	}
	var manifest snapshotManifest
	manifestPath := filepath.Join(backup, "snapshot.json")
	err = e.checkPath(manifestPath)
	if err != nil {
		return fmt.Errorf("rollbackManifestPath: %w", err)
	}
	err = readJSON(manifestPath, &manifest)
	if err != nil {
		return fmt.Errorf("rollbackManifest: %w", err)
	}
	if transaction.Phase == "healthy" {
		return errors.New("cannot roll back committed candidate")
	}
	if !manifest.IsComplete {
		return errors.New("rollback snapshot incomplete")
	}
	backupBinary := filepath.Join(backup, "bin", "darkrun")
	err = e.checkPath(backupBinary)
	if err != nil {
		return fmt.Errorf("rollbackBinary: %w", err)
	}
	actualHash, err := hashFile(backupBinary)
	if err != nil {
		return fmt.Errorf("rollbackHash: %w", err)
	}
	if actualHash != manifest.PreviousHash {
		return errors.New("rollback binary checksum mismatch")
	}
	err = e.validateDatabaseSnapshot(backup, manifest)
	if err != nil {
		return fmt.Errorf("databaseValidate: %w", err)
	}
	transaction.Phase = "restoring"
	err = e.writeJSON(statePath, transaction)
	if err != nil {
		return fmt.Errorf("rollbackMark: %w", err)
	}
	if e.DatabaseBackup != nil {
		err = e.DatabaseBackup.Restore(ctx, filepath.Join(backup, "postgres.dump"))
		if err != nil {
			return fmt.Errorf("databaseRestore: %w", err)
		}
	}
	for _, rel := range snapshotPaths {
		err = e.restorePath(backup, rel)
		if err != nil {
			return fmt.Errorf("rollbackRestore: %w", err)
		}
	}
	// Cached content may have been migrated by the candidate. Retain it for
	// diagnostics and let the matching old executable rebuild from installed data.
	err = e.preserveFailed(backup, "runtime/darkspin/cache")
	if err != nil {
		return fmt.Errorf("rollbackCache: %w", err)
	}
	err = os.Remove(statePath)
	if err != nil {
		return fmt.Errorf("rollbackClear: %w", err)
	}
	err = syncDirectory(e.root)
	if err != nil {
		return fmt.Errorf("rollbackSync: %w", err)
	}
	return nil
}

func (e *Storage) preserveFailed(backup, rel string) error {
	target := e.path(filepath.FromSlash(rel))
	failed := filepath.Join(backup, "failed", filepath.FromSlash(rel))
	for _, path := range []string{target, failed} {
		err := e.checkPath(path)
		if err != nil {
			return fmt.Errorf("failedCheck: %w", err)
		}
	}
	fi, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failedTarget: %w", err)
	}
	if !fi.IsDir() && !fi.Mode().IsRegular() {
		return errors.New("failed candidate path is not regular file or directory")
	}
	failedFI, err := os.Lstat(failed)
	if err == nil {
		if !failedFI.IsDir() && !failedFI.Mode().IsRegular() {
			return errors.New("preserved diagnostic path is not regular file or directory")
		}
		err = e.removeTree(target)
		if err != nil {
			return fmt.Errorf("failedRemove: %w", err)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failedStat: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(failed), 0700)
	if err != nil {
		return fmt.Errorf("failedDirectory: %w", err)
	}
	err = os.Rename(target, failed)
	if err != nil {
		return fmt.Errorf("failedRename: %w", err)
	}
	err = syncDirectory(filepath.Dir(failed))
	if err != nil {
		return fmt.Errorf("failedSync: %w", err)
	}
	err = syncDirectory(filepath.Dir(target))
	if err != nil {
		return fmt.Errorf("targetSync: %w", err)
	}
	return nil
}

func (e *Storage) restorePath(backup, rel string) error {
	source := filepath.Join(backup, filepath.FromSlash(rel))
	target := e.path(filepath.FromSlash(rel))
	stage := target + ".restore"
	err := e.removeTree(stage)
	if err != nil {
		return fmt.Errorf("restoreClean: %w", err)
	}
	fi, err := os.Lstat(source)
	isAbsent := errors.Is(err, os.ErrNotExist)
	if err != nil && !isAbsent {
		return fmt.Errorf("restoreStat: %w", err)
	}
	if !isAbsent {
		if fi.IsDir() {
			err = e.copyTree(source, stage)
		} else {
			err = e.copyFile(source, stage, fi.Mode().Perm())
		}
		if err != nil {
			return fmt.Errorf("restoreStage: %w", err)
		}
	}
	if rel == "bin/darkrun" {
		// Keep the bootstrap entrypoint present across every crash boundary.
		// Linux can atomically replace the executable while bootstrap runs it.
		err = e.preserveFailedBinary(backup)
	} else {
		err = e.preserveFailed(backup, rel)
	}
	if err != nil {
		return fmt.Errorf("restorePreserve: %w", err)
	}
	if isAbsent {
		return nil
	}
	err = os.MkdirAll(filepath.Dir(target), 0700)
	if err != nil {
		return fmt.Errorf("restoreDirectory: %w", err)
	}
	err = os.Rename(stage, target)
	if err != nil {
		return fmt.Errorf("restoreRename: %w", err)
	}
	err = syncDirectory(filepath.Dir(target))
	if err != nil {
		return fmt.Errorf("restoreSync: %w", err)
	}
	return nil
}

func (e *Storage) preserveFailedBinary(backup string) error {
	target := e.path("bin", "darkrun")
	failed := filepath.Join(backup, "failed", "bin", "darkrun")
	for _, path := range []string{target, failed, failed + ".tmp"} {
		err := e.checkPath(path)
		if err != nil {
			return fmt.Errorf("failedBinaryPath: %w", err)
		}
	}
	fi, err := os.Lstat(failed)
	if err == nil {
		if !fi.Mode().IsRegular() {
			return errors.New("preserved executable is not a regular file")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failedBinaryStat: %w", err)
	}
	fi, err = os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failedExecutableStat: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return errors.New("candidate executable is not a regular file")
	}
	err = e.copyFile(target, failed+".tmp", fi.Mode().Perm())
	if err != nil {
		return fmt.Errorf("failedBinaryCopy: %w", err)
	}
	err = os.Rename(failed+".tmp", failed)
	if err != nil {
		return fmt.Errorf("failedBinaryRename: %w", err)
	}
	err = e.syncParents(filepath.Dir(failed))
	if err != nil {
		return fmt.Errorf("failedBinarySync: %w", err)
	}
	return nil
}

// ConfirmHealthy durably commits the first boot. A crash after this marker is
// committed must never restore old databases over accepted gameplay progress.
func (e *Storage) ConfirmHealthy() error {
	statePath := e.path("firstboot.json")
	err := e.checkPath(statePath)
	if err != nil {
		return fmt.Errorf("healthyCheck: %w", err)
	}
	var transaction updateTransaction
	err = readJSON(statePath, &transaction)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("healthyRead: %w", err)
	}
	transaction.Phase = "healthy"
	err = e.writeJSON(statePath, transaction)
	if err != nil {
		return fmt.Errorf("healthyCommit: %w", err)
	}
	return nil
}

// Prune is housekeeping after a healthy start. An active rollback transaction
// prevents all pruning, including removal of its only matching snapshot.
func (e *Storage) Prune() error {
	statePath := e.path("firstboot.json")
	err := e.checkPath(statePath)
	if err != nil {
		return fmt.Errorf("pruneStatePath: %w", err)
	}
	var transaction updateTransaction
	err = readJSON(statePath, &transaction)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("pruneState: %w", err)
	}
	if err == nil {
		if transaction.Phase != "healthy" {
			return errors.New("cannot prune an active rollback transaction")
		}
		err = os.Remove(statePath)
		if err != nil {
			return fmt.Errorf("pruneClear: %w", err)
		}
		err = syncDirectory(e.root)
		if err != nil {
			return fmt.Errorf("pruneSync: %w", err)
		}
	}
	root := e.path("backup")
	err = e.checkPath(root)
	if err != nil {
		return fmt.Errorf("pruneCheck: %w", err)
	}
	days, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("pruneRead: %w", err)
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -e.retentionDays)
	for _, day := range days {
		if !day.IsDir() {
			continue
		}
		dayPath := filepath.Join(root, day.Name())
		err = e.checkPath(dayPath)
		if err != nil {
			return fmt.Errorf("pruneDay: %w", err)
		}
		backups, readErr := os.ReadDir(dayPath)
		if readErr != nil {
			return fmt.Errorf("pruneBackups: %w", readErr)
		}
		for _, backup := range backups {
			if !backup.IsDir() {
				continue
			}
			path := filepath.Join(dayPath, backup.Name())
			var manifest snapshotManifest
			err = e.checkPath(path)
			if err != nil {
				return fmt.Errorf("prunePath: %w", err)
			}
			manifestPath := filepath.Join(path, "snapshot.json")
			err = e.checkPath(manifestPath)
			if err != nil {
				return fmt.Errorf("pruneManifestPath: %w", err)
			}
			err = readJSON(manifestPath, &manifest)
			if err != nil {
				continue
			} // Incomplete/foreign snapshots are never removed.
			created, parseErr := time.Parse(time.RFC3339Nano, manifest.Created)
			if parseErr != nil || !manifest.IsComplete || !created.Before(cutoff) {
				continue
			}
			err = e.removeTree(path)
			if err != nil {
				return fmt.Errorf("pruneRemove: %w", err)
			}
		}
	}
	return nil
}
