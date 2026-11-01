package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/sporenet/sqlite"
)

// Run once in the launcher's initialization worker, including remote-only starts.
func (e *App) cleanupExpiredLocalProfiles(ctx context.Context, basePath string) {
	paths, err := resolveSpinnerPaths(basePath)
	if err != nil {
		e.log(fmt.Sprintf("Profile expiry paths unavailable: %v", err))
		return
	}
	for _, databasePath := range []string{paths.userPath, modDatabasePath(paths)} {
		count, purgeErr := purgeExpiredLocalProfiles(ctx, databasePath)
		if purgeErr != nil {
			e.log(fmt.Sprintf("Profile expiry failed database=%q: %v", databasePath, purgeErr))
			continue
		}
		if count > 0 {
			e.log(fmt.Sprintf("Expired deleted profiles removed database=%q count=%d", databasePath, count))
		}
	}
}

func purgeExpiredLocalProfiles(ctx context.Context, databasePath string) (int64, error) {
	fi, err := os.Stat(databasePath)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("expiryStat: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return 0, errors.New("profile database is not a regular file")
	}
	repository, err := sqlite.New(ctx, databasePath, sqlite.Options{})
	if err != nil {
		return 0, fmt.Errorf("expiryOpen: %w", err)
	}
	count, purgeErr := sporenet.PurgeExpiredProfiles(ctx, repository, time.Now())
	closeErr := repository.Close()
	if purgeErr != nil || closeErr != nil {
		return 0, fmt.Errorf("expiryPurge: %w", errors.Join(purgeErr, closeErr))
	}
	return count, nil
}
