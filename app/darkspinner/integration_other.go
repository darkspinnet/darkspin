//go:build !windows

package main

import "errors"

func completePendingInstallation([]string) error {
	return nil
}

func inspectLauncherIntegrations(string, string) (LauncherIntegrationStatus, error) {
	return LauncherIntegrationStatus{
		Message: "Launcher integrations are only available on Windows.",
	}, nil
}

func repairLauncherIntegration(string, string, string) error {
	return errors.New("launcher integrations are only available on Windows")
}

func removeLauncherIntegration(string, string, string) error {
	return errors.New("launcher integrations are only available on Windows")
}

func scheduleLauncherUninstall(string, string, int) error {
	return errors.New("launcher uninstall is only available on Windows")
}
