//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

func startDetachedProcess(executablePath string, arguments []string) error {
	cmd := exec.Command(executablePath, arguments...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
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
