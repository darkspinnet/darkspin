//go:build windows

package main

import (
	"fmt"
	"os/exec"
)

func openFirewallSettings() error {
	command := exec.Command(
		"control.exe", "/name", "Microsoft.WindowsFirewall", "/page", "pageConfigureApps",
	)
	err := command.Start()
	if err != nil {
		return fmt.Errorf("settingsStart: %w", err)
	}
	err = command.Process.Release()
	if err != nil {
		return fmt.Errorf("settingsRelease: %w", err)
	}
	return nil
}
