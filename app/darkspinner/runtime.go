//go:build !bindings

package main

import (
	"fmt"
	"os"
)

func prepareRuntime() error {
	basePath, err := executableDirectory()
	if err != nil {
		return fmt.Errorf("executableDirectory: %w", err)
	}
	err = os.Chdir(basePath)
	if err != nil {
		return fmt.Errorf("workingDirectory: %w", err)
	}
	return nil
}
