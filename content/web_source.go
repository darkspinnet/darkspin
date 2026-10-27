package content

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

// ValidateWebSource checks access to the package indexes needed to rebuild web
// assets. It never extracts resources or writes into the game installation.
func ValidateWebSource(ctx context.Context, gamePath string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	installPath, err := webInstallPath(gamePath)
	if err != nil {
		return fmt.Errorf("installResolve: %w", err)
	}
	// Loot images also consume these packages independently of webAssetRecipe.
	packages := map[string]struct{}{"UI.package": {}, "AssetData_Binary.package": {}}
	for _, asset := range webAssetRecipe {
		packages[asset.Package] = struct{}{}
	}
	for packageName := range packages {
		err = ctx.Err()
		if err != nil {
			return fmt.Errorf("sourceContext: %w", err)
		}
		err = dbpf.VerifyPath(filepath.Join(installPath, "Data", packageName))
		if err != nil {
			return fmt.Errorf("webPackage[%s]: %w", packageName, err)
		}
	}
	return nil
}
