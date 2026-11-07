package ds

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// A directory rewrite preserves authored source and diagnostic-only resources.
// Compiling is reserved for package creation, where every Lua source is rebuilt.
func copyLuaSourceResource(ctx context.Context, sourcePath, destinationPath string) error {
	err := validateLuaResource(ctx, sourcePath, resourceDefinitionIdentity(filepath.Base(sourcePath)))
	if err != nil {
		return fmt.Errorf("sourceValidate: %w", err)
	}
	paths, err := luaResourceSidecarPaths(filepath.Dir(sourcePath), sourcePath)
	if err != nil {
		return fmt.Errorf("sidecarList: %w", err)
	}
	paths = append(paths, filepath.Base(sourcePath))
	err = os.MkdirAll(filepath.Dir(destinationPath), 0o755)
	if err != nil {
		return fmt.Errorf("destinationMkdir: %w", err)
	}
	for _, path := range paths {
		err = ctx.Err()
		if err != nil {
			return fmt.Errorf("copyContext: %w", err)
		}
		payload, readErr := os.ReadFile(filepath.Join(filepath.Dir(sourcePath), path))
		if readErr != nil {
			return fmt.Errorf("sidecarRead: %w", readErr)
		}
		name := filepath.Base(path)
		if name == filepath.Base(sourcePath) {
			name = filepath.Base(destinationPath)
		}
		err = writeLuaFile(filepath.Join(filepath.Dir(destinationPath), name), payload)
		if err != nil {
			return fmt.Errorf("sidecarWrite: %w", err)
		}
	}
	return nil
}
