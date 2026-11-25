package bootstrap

import (
	"context"
	"errors"
	"fmt"
)

// UpdateOverview is a read-only comparison with the configured manifest. It
// never downloads the candidate executable or creates update state.
type UpdateOverview struct {
	Version   string
	BuildID   string
	IsCurrent bool
}

func CheckOverview(ctx context.Context, manifestURL, executablePath string) (UpdateOverview, error) {
	manifest, err := fetchManifest(ctx, manifestURL)
	if err != nil {
		return UpdateOverview{}, fmt.Errorf("overviewManifest: %w", err)
	}
	if !safeResultText(manifest.Version, 128) || !safeResultText(manifest.BuildID, 256) {
		return UpdateOverview{}, errors.New("invalid candidate identity label")
	}
	err = ctx.Err()
	if err != nil {
		return UpdateOverview{}, fmt.Errorf("overviewContext: %w", err)
	}
	currentHash, err := hashFile(executablePath)
	if err != nil {
		return UpdateOverview{}, fmt.Errorf("overviewHash: %w", err)
	}
	err = ctx.Err()
	if err != nil {
		return UpdateOverview{}, fmt.Errorf("overviewComplete: %w", err)
	}
	// Match the updater's decision, including patch and same-version rebuilds.
	return UpdateOverview{Version: manifest.Version, BuildID: manifest.BuildID,
		IsCurrent: manifest.SHA256 == currentHash}, nil
}
