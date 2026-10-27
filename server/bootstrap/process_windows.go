//go:build windows

package bootstrap

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

func configureChild(cmd *exec.Cmd) {}

func signalChild(cmd *exec.Cmd) error {
	return errors.New("graceful bootstrap shutdown requires Linux; stop through maintenance control")
}

func killChild(cmd *exec.Cmd) error {
	err := cmd.Process.Kill()
	if err != nil {
		return fmt.Errorf("processKill: %w", err)
	}
	return nil
}

func installedBinaryName() string { return "darkrun.exe" }

func executableExists(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("binaryStat: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("binary is not a regular file: %s", path)
	}
	return nil
}
