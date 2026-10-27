package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (e *Storage) snapshotDatabase(ctx context.Context, destination string, manifest *snapshotManifest) error {
	if e.DatabaseBackup == nil {
		return nil
	}
	path := filepath.Join(destination, "postgres.dump")
	err := e.checkPath(path)
	if err != nil {
		return fmt.Errorf("dumpPath: %w", err)
	}
	err = e.DatabaseBackup.Dump(ctx, path)
	if err != nil {
		return fmt.Errorf("dumpCapture: %w", err)
	}
	checksum, err := hashFile(path)
	if err != nil {
		return fmt.Errorf("dumpHash: %w", err)
	}
	err = e.syncParents(destination)
	if err != nil {
		return fmt.Errorf("dumpSync: %w", err)
	}
	manifest.DatabaseTarget = e.DatabaseBackup.Identity()
	manifest.DatabaseHash = checksum
	return nil
}

func (e *Storage) validateDatabaseSnapshot(backup string, manifest snapshotManifest) error {
	if e.DatabaseBackup == nil && manifest.DatabaseHash == "" && manifest.DatabaseTarget == "" {
		return nil
	}
	if e.DatabaseBackup == nil || manifest.DatabaseHash == "" || manifest.DatabaseTarget == "" {
		return errors.New("snapshot database backend does not match; recover legacy SQLite snapshots with their original binary")
	}
	if manifest.DatabaseTarget != e.DatabaseBackup.Identity() {
		return errors.New("snapshot PostgreSQL destination differs from the configured connection")
	}
	path := filepath.Join(backup, "postgres.dump")
	err := e.checkPath(path)
	if err != nil {
		return fmt.Errorf("dumpPath: %w", err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("dumpStat: %w", err)
	}
	if !fi.Mode().IsRegular() || fi.Size() == 0 {
		return errors.New("snapshot PostgreSQL dump is not a nonempty regular file")
	}
	checksum, err := hashFile(path)
	if err != nil {
		return fmt.Errorf("dumpHash: %w", err)
	}
	if checksum != manifest.DatabaseHash {
		return errors.New("snapshot PostgreSQL dump checksum mismatch")
	}
	return nil
}
