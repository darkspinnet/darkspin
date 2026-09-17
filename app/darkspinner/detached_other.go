//go:build !windows

package main

import (
	"fmt"
	"os/exec"
)

func startDetachedProcess(executablePath string, arguments []string) error {
	cmd := exec.Command(executablePath, arguments...)
	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("processStart: %w", err)
	}
	err = cmd.Process.Release()
	if err != nil {
		return fmt.Errorf("processRelease: %w", err)
	}
	return nil
}
