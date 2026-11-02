//go:build scenario

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
	"modernc.org/sqlite"
)

// SQLite reads a coherent committed view, including WAL pages. It never
// checkpoints or changes the source database; the ordinary locked copy then
// preserves the resulting offline input and its exact-byte digest.
func snapshotLiveScenarioContent(ctx context.Context, sourcePath, destinationPath string) (scenario.Artifact, error) {
	fi, err := os.Lstat(sourcePath)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("backupStat: %w", err)
	}
	if !fi.Mode().IsRegular() || fi.Size() < 100 {
		return scenario.Artifact{}, errors.New("scenario content source is not a SQLite database file")
	}
	w, err := os.CreateTemp(filepath.Dir(destinationPath), "content-backup-*.db")
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("backupAllocate: %w", err)
	}
	path := w.Name()
	closeErr := w.Close()
	if closeErr != nil {
		return scenario.Artifact{}, fmt.Errorf("backupReserve: %w", closeErr)
	}
	defer removeScenarioBackup(path)
	sourceURL := url.URL{Scheme: "file", Path: filepath.ToSlash(sourcePath)}
	if filepath.VolumeName(sourcePath) != "" {
		sourceURL.Path = "/" + sourceURL.Path
	}
	query := sourceURL.Query()
	query.Set("mode", "ro")
	query.Set("_pragma", "busy_timeout(1000)")
	sourceURL.RawQuery = query.Encode()
	database, err := sql.Open("sqlite", sourceURL.String())
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("backupOpen: %w", err)
	}
	connection, err := database.Conn(ctx)
	if err != nil {
		closeErr = database.Close()
		return scenario.Artifact{}, fmt.Errorf("backupConnect: %w", errors.Join(err, closeErr))
	}
	err = connection.Raw(func(raw any) error { return copyScenarioBackupPages(ctx, raw, path) })
	connectionErr := connection.Close()
	databaseErr := database.Close()
	if err != nil || connectionErr != nil || databaseErr != nil {
		return scenario.Artifact{}, fmt.Errorf("backupCopy: %w", errors.Join(err, connectionErr, databaseErr))
	}
	r, err := openScenarioContent(path)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("backupLock: %w", err)
	}
	artifact, copyErr := copyScenarioContent(ctx, r, path, destinationPath)
	closeErr = r.Close()
	if copyErr != nil || closeErr != nil {
		return scenario.Artifact{}, fmt.Errorf("backupSnapshot: %w", errors.Join(copyErr, closeErr))
	}
	return artifact, nil
}

func copyScenarioBackupPages(ctx context.Context, raw any, destinationPath string) error {
	connection, isSupported := raw.(interface {
		NewBackup(string) (*sqlite.Backup, error)
	})
	if !isSupported {
		return errors.New("SQLite connection does not support online backup")
	}
	destinationURL := url.URL{Scheme: "file", Path: filepath.ToSlash(destinationPath)}
	if filepath.VolumeName(destinationPath) != "" {
		destinationURL.Path = "/" + destinationURL.Path
	}
	backup, err := connection.NewBackup(destinationURL.String())
	if err != nil {
		return fmt.Errorf("backupStart: %w", err)
	}
	err = stepScenarioBackup(ctx, backup)
	finishErr := backup.Finish()
	if err != nil || finishErr != nil {
		return fmt.Errorf("backupFinish: %w", errors.Join(err, finishErr))
	}
	return nil
}

func stepScenarioBackup(ctx context.Context, backup *sqlite.Backup) error {
	for {
		err := ctx.Err()
		if err != nil {
			return fmt.Errorf("backupContext: %w", err)
		}
		isMore, stepErr := backup.Step(128)
		if stepErr == nil {
			if !isMore {
				return nil
			}
			continue
		}
		var failure *sqlite.Error
		if !errors.As(stepErr, &failure) || (failure.Code()&255 != 5 && failure.Code()&255 != 6) {
			return fmt.Errorf("backupStep: %w", stepErr)
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("backupWait: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func removeScenarioBackup(path string) {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		count, outputErr := fmt.Fprintln(os.Stderr, "Scenario temporary backup cleanup:", err)
		if outputErr != nil || count == 0 { /* The snapshot result remains available without stderr. */
		}
	}
}
