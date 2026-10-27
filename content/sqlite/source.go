package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

// ValidateSource checks build provenance and access to each required database
// source package, even when an existing generated cache is already current.
func ValidateSource(ctx context.Context, gamePath string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	installPath, err := resolveInstallPath(gamePath)
	if err != nil {
		return fmt.Errorf("installResolve: %w", err)
	}
	err = validateSourceVersion(installPath)
	if err != nil {
		return fmt.Errorf("sourceVersion: %w", err)
	}
	for _, spec := range buildPackages {
		err = ctx.Err()
		if err != nil {
			return fmt.Errorf("sourceContext: %w", err)
		}
		err = dbpf.VerifyPath(filepath.Join(installPath, spec.relativePath))
		if err != nil {
			return fmt.Errorf("sourcePackage[%s]: %w", spec.name, err)
		}
	}
	return nil
}
