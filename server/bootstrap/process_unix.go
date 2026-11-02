//go:build linux || darwin

package bootstrap

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func configureChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func signalChild(cmd *exec.Cmd) error {
	err := cmd.Process.Signal(syscall.SIGTERM)
	if err != nil {
		return fmt.Errorf("processSignal: %w", err)
	}
	return nil
}

func killChild(cmd *exec.Cmd) error {
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if err == syscall.ESRCH {
		return nil
	}
	if err != nil {
		return fmt.Errorf("groupKill: %w", err)
	}
	return nil
}

func installedBinaryName() string { return "darkrun" }

func executableExists(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("binaryStat: %w", err)
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("binary is not executable: %s", path)
	}
	return nil
}
