// Package runtime shares private content preparation across server hosts.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/darkspinnet/darkspin/content"
	"github.com/darkspinnet/darkspin/content/sqlite"
)

// Options identifies the private cache and an optional source installation.
// Existing verified caches do not require access to GamePath.
type Options struct {
	GamePath      string
	DatabasePath  string
	StaticPath    string
	OnProgress    func(sqlite.BuildProgress)
	OnStageTiming func(sqlite.BuildStageTiming)
}

// Ensure verifies private runtime content, rebuilding stale or missing products
// from the installation when necessary. Failed imports retain the old database.
func Ensure(ctx context.Context, req Options) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if req.DatabasePath == "" {
		return errors.New("empty database path")
	}
	staticPath := req.StaticPath
	if staticPath == "" {
		staticPath = filepath.Join(filepath.Dir(req.DatabasePath), "www", "static")
	}
	_, databaseErr := sqlite.Verify(ctx, req.DatabasePath)
	webErr := content.VerifyWeb(ctx, staticPath)
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("prepareContext: %w", err)
	}
	if webErr != nil {
		err = content.PrepareWeb(ctx, content.WebOptions{GamePath: req.GamePath, StaticPath: staticPath})
		if err != nil {
			return preparationError(req.DatabasePath, fmt.Errorf("webPrepare: %w", errors.Join(webErr, err)))
		}
		err = content.VerifyWeb(ctx, staticPath)
		if err != nil {
			return preparationError(req.DatabasePath, fmt.Errorf("webVerify: %w", err))
		}
	}
	if databaseErr == nil {
		return nil
	}
	err = sqlite.Build(ctx, sqlite.BuildOptions{
		GamePath: req.GamePath, DatabasePath: req.DatabasePath,
		OnProgress: req.OnProgress, OnStageTiming: req.OnStageTiming,
		IsReplacementAllowed: true,
	})
	if err != nil {
		return preparationError(req.DatabasePath, fmt.Errorf("databaseBuild: %w", errors.Join(databaseErr, err)))
	}
	return nil
}

func preparationError(databasePath string, err error) error {
	return fmt.Errorf(
		"private content is missing or stale; set DARKSPIN_GAME_PATH (or --game-path) to a readable "+
			"build-103 installation to rebuild automatically on startup; in Docker, mount that installation read-only "+
			"and use its container path. Keep that source mounted so the server can regenerate %q "+
			"and its adjacent web cache after updates: %w",
		databasePath, err,
	)
}
