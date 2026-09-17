//go:build windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

const managedLaunchEnvironment = "DARKSPIN_DARKSPINNER_LAUNCH"

func completePendingInstallation(arguments []string) error {
	isStartMenuRequested := hasLaunchArgument(arguments, "install-start-menu-shortcut")
	isDesktopRequested := hasLaunchArgument(arguments, "install-desktop-shortcut")
	isSteamRequested := hasLaunchArgument(arguments, "install-steam-launch")
	if !isStartMenuRequested && !isDesktopRequested && !isSteamRequested {
		return nil
	}
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("integrationExecutable: %w", err)
	}
	executablePath, err = filepath.Abs(executablePath)
	if err != nil {
		return fmt.Errorf("integrationPath: %w", err)
	}
	integrationErrors := make([]error, 0, 3)
	if isSteamRequested {
		err = installSteamLaunchProxy(filepath.Dir(executablePath))
		if err != nil {
			integrationErrors = append(integrationErrors, fmt.Errorf("steamIntegration: %w", err))
		}
	}
	if isStartMenuRequested {
		err = createKnownFolderShortcut(executablePath, windows.FOLDERID_Programs)
		if err != nil {
			integrationErrors = append(integrationErrors, fmt.Errorf("startMenuShortcut: %w", err))
		}
	}
	if isDesktopRequested {
		err = createKnownFolderShortcut(executablePath, windows.FOLDERID_Desktop)
		if err != nil {
			integrationErrors = append(integrationErrors, fmt.Errorf("desktopShortcut: %w", err))
		}
	}
	return errors.Join(integrationErrors...)
}

func createKnownFolderShortcut(executablePath string, folderID *windows.KNOWNFOLDERID) error {
	folderPath, err := windows.KnownFolderPath(folderID, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return fmt.Errorf("shortcutFolder: %w", err)
	}
	shortcutPath := filepath.Join(folderPath, "Darkspinner.lnk")
	scriptWriter, err := os.CreateTemp("", "darkspinner-shortcut-*.ps1")
	if err != nil {
		return fmt.Errorf("shortcutScriptCreate: %w", err)
	}
	scriptPath := scriptWriter.Name()
	defer os.Remove(scriptPath)
	script := `param(
    [Parameter(Mandatory=$true)][string]$ExecutablePath,
    [Parameter(Mandatory=$true)][string]$ShortcutPath
)
$ErrorActionPreference = 'Stop'
$Shell = New-Object -ComObject WScript.Shell
$Shortcut = $Shell.CreateShortcut($ShortcutPath)
$Shortcut.TargetPath = $ExecutablePath
$Shortcut.WorkingDirectory = Split-Path -Parent $ExecutablePath
$Shortcut.IconLocation = $ExecutablePath + ',0'
$Shortcut.Description = 'Darkspinner launcher for Game'
$Shortcut.Save()
`
	_, err = scriptWriter.WriteString(script)
	if err != nil {
		_ = scriptWriter.Close()
		return fmt.Errorf("shortcutScriptWrite: %w", err)
	}
	err = scriptWriter.Close()
	if err != nil {
		return fmt.Errorf("shortcutScriptClose: %w", err)
	}
	command := exec.Command(
		"powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", scriptPath, "-ExecutablePath", executablePath, "-ShortcutPath", shortcutPath,
	)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := command.CombinedOutput()
	if err != nil {
		diagnostic := strings.TrimSpace(string(output))
		if diagnostic != "" {
			return fmt.Errorf("shortcutCreate: %w: %s", err, diagnostic)
		}
		return fmt.Errorf("shortcutCreate: %w", err)
	}
	return nil
}

func installSteamLaunchProxy(gamePath string) error {
	if len(embeddedSteamProxy) == 0 {
		return errors.New("Steam launch proxy is unavailable in this build")
	}
	binaryPath := filepath.Join(gamePath, "DarksporeBin")
	proxyPath := filepath.Join(binaryPath, "VERSION.dll")
	contents, err := os.ReadFile(proxyPath)
	if err == nil {
		if bytes.Equal(contents, embeddedSteamProxy) {
			return nil
		}
		if !bytes.Contains(contents, []byte(proxyMarker)) {
			return errors.New("DarksporeBin\\VERSION.dll already exists and is not owned by Darkspinner")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("proxyRead: %w", err)
	}
	stagedWriter, err := os.CreateTemp(binaryPath, ".darkspinner-version-*.dll")
	if err != nil {
		return fmt.Errorf("proxyStageCreate: %w", err)
	}
	stagedPath := stagedWriter.Name()
	defer os.Remove(stagedPath)
	_, err = stagedWriter.Write(embeddedSteamProxy)
	if err != nil {
		_ = stagedWriter.Close()
		return fmt.Errorf("proxyStageWrite: %w", err)
	}
	err = stagedWriter.Sync()
	if err != nil {
		_ = stagedWriter.Close()
		return fmt.Errorf("proxyStageSync: %w", err)
	}
	err = stagedWriter.Close()
	if err != nil {
		return fmt.Errorf("proxyStageClose: %w", err)
	}
	stagedPointer, err := windows.UTF16PtrFromString(stagedPath)
	if err != nil {
		return fmt.Errorf("proxyStagePath: %w", err)
	}
	proxyPointer, err := windows.UTF16PtrFromString(proxyPath)
	if err != nil {
		return fmt.Errorf("proxyDestinationPath: %w", err)
	}
	err = windows.MoveFileEx(stagedPointer, proxyPointer, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
	if err != nil {
		return fmt.Errorf("proxyInstall: %w", err)
	}
	return nil
}

func inspectLauncherIntegrations(executablePath string, basePath string) (LauncherIntegrationStatus, error) {
	status := LauncherIntegrationStatus{IsManagementSupported: true}
	startMenuPath, err := knownFolderShortcutPath(windows.FOLDERID_Programs)
	if err != nil {
		return status, fmt.Errorf("startMenuPath: %w", err)
	}
	desktopPath, err := knownFolderShortcutPath(windows.FOLDERID_Desktop)
	if err != nil {
		return status, fmt.Errorf("desktopPath: %w", err)
	}
	status.IsStartMenuInstalled = isPathPresent(startMenuPath)
	status.IsDesktopInstalled = isPathPresent(desktopPath)
	proxyPath := filepath.Join(basePath, "DarksporeBin", "VERSION.dll")
	contents, err := os.ReadFile(proxyPath)
	if err == nil {
		status.IsSteamLaunchInstalled = bytes.Contains(contents, []byte(proxyMarker))
	} else if !errors.Is(err, os.ErrNotExist) {
		return status, fmt.Errorf("steamProxyRead: %w", err)
	}
	status.Message = "Launcher integrations are ready to manage."
	return status, nil
}

func repairLauncherIntegration(kind string, executablePath string, basePath string) error {
	switch kind {
	case launcherIntegrationStartMenu:
		return createKnownFolderShortcut(executablePath, windows.FOLDERID_Programs)
	case launcherIntegrationDesktop:
		return createKnownFolderShortcut(executablePath, windows.FOLDERID_Desktop)
	case launcherIntegrationSteam:
		return installSteamLaunchProxy(basePath)
	default:
		return errors.New("unsupported launcher integration")
	}
}

func removeLauncherIntegration(kind string, executablePath string, basePath string) error {
	switch kind {
	case launcherIntegrationStartMenu:
		shortcutPath, err := knownFolderShortcutPath(windows.FOLDERID_Programs)
		if err != nil {
			return fmt.Errorf("startMenuPath: %w", err)
		}
		return removeOwnedShortcut(shortcutPath, executablePath)
	case launcherIntegrationDesktop:
		shortcutPath, err := knownFolderShortcutPath(windows.FOLDERID_Desktop)
		if err != nil {
			return fmt.Errorf("desktopPath: %w", err)
		}
		return removeOwnedShortcut(shortcutPath, executablePath)
	case launcherIntegrationSteam:
		return removeSteamLaunchProxy(basePath)
	default:
		return errors.New("unsupported launcher integration")
	}
}

func knownFolderShortcutPath(folderID *windows.KNOWNFOLDERID) (string, error) {
	folderPath, err := windows.KnownFolderPath(folderID, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return "", fmt.Errorf("knownFolder: %w", err)
	}
	return filepath.Join(folderPath, "Darkspinner.lnk"), nil
}

func isPathPresent(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func removeOwnedShortcut(shortcutPath string, executablePath string) error {
	if !isPathPresent(shortcutPath) {
		return nil
	}
	scriptWriter, err := os.CreateTemp("", "darkspinner-shortcut-remove-*.ps1")
	if err != nil {
		return fmt.Errorf("shortcutRemoveCreate: %w", err)
	}
	scriptPath := scriptWriter.Name()
	defer os.Remove(scriptPath)
	script := `param(
    [Parameter(Mandatory=$true)][string]$ExecutablePath,
    [Parameter(Mandatory=$true)][string]$ShortcutPath
)
$ErrorActionPreference = 'Stop'
if (-not (Test-Path -LiteralPath $ShortcutPath)) { exit 0 }
$Shell = New-Object -ComObject WScript.Shell
$Shortcut = $Shell.CreateShortcut($ShortcutPath)
if (-not [string]::Equals([IO.Path]::GetFullPath($Shortcut.TargetPath), [IO.Path]::GetFullPath($ExecutablePath), [StringComparison]::OrdinalIgnoreCase)) {
    throw 'The shortcut is not owned by this Darkspinner executable.'
}
Remove-Item -LiteralPath $ShortcutPath -Force
`
	_, err = scriptWriter.WriteString(script)
	if err != nil {
		_ = scriptWriter.Close()
		return fmt.Errorf("shortcutRemoveWrite: %w", err)
	}
	err = scriptWriter.Close()
	if err != nil {
		return fmt.Errorf("shortcutRemoveClose: %w", err)
	}
	command := exec.Command(
		"powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", scriptPath, "-ExecutablePath", executablePath, "-ShortcutPath", shortcutPath,
	)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := command.CombinedOutput()
	if err != nil {
		diagnostic := strings.TrimSpace(string(output))
		if diagnostic != "" {
			return fmt.Errorf("shortcutRemove: %w: %s", err, diagnostic)
		}
		return fmt.Errorf("shortcutRemove: %w", err)
	}
	return nil
}

func removeSteamLaunchProxy(basePath string) error {
	proxyPath := filepath.Join(basePath, "DarksporeBin", "VERSION.dll")
	contents, err := os.ReadFile(proxyPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("proxyRead: %w", err)
	}
	if !bytes.Contains(contents, []byte(proxyMarker)) {
		return errors.New("DarksporeBin\\VERSION.dll is not owned by Darkspinner")
	}
	err = os.Remove(proxyPath)
	if err != nil {
		return fmt.Errorf("proxyRemove: %w", err)
	}
	return nil
}

func scheduleLauncherUninstall(executablePath string, basePath string, processID int) error {
	cleanExecutablePath, err := filepath.Abs(executablePath)
	if err != nil {
		return fmt.Errorf("uninstallExecutablePath: %w", err)
	}
	cleanBasePath, err := filepath.Abs(basePath)
	if err != nil {
		return fmt.Errorf("uninstallBasePath: %w", err)
	}
	if !strings.EqualFold(filepath.Dir(cleanExecutablePath), cleanBasePath) {
		return errors.New("launcher executable is outside the game root")
	}
	if !isLocalGameRoot(cleanBasePath) {
		return errors.New("launcher is not beside a valid game installation")
	}
	runtimePath := filepath.Join(cleanBasePath, "darkspin")
	if !strings.EqualFold(filepath.Dir(runtimePath), cleanBasePath) {
		return errors.New("runtime directory is outside the game root")
	}
	scriptWriter, err := os.CreateTemp("", "darkspinner-uninstall-*.ps1")
	if err != nil {
		return fmt.Errorf("uninstallScriptCreate: %w", err)
	}
	scriptPath := scriptWriter.Name()
	script := `param(
    [Parameter(Mandatory=$true)][int]$ProcessID,
    [Parameter(Mandatory=$true)][string]$RuntimePath,
    [Parameter(Mandatory=$true)][string]$ExecutablePath,
    [Parameter(Mandatory=$true)][string]$ScriptPath
)
$ErrorActionPreference = 'SilentlyContinue'
Wait-Process -Id $ProcessID
Remove-Item -LiteralPath $RuntimePath -Recurse -Force
Remove-Item -LiteralPath $ExecutablePath -Force
Remove-Item -LiteralPath $ScriptPath -Force
`
	_, err = scriptWriter.WriteString(script)
	if err != nil {
		_ = scriptWriter.Close()
		_ = os.Remove(scriptPath)
		return fmt.Errorf("uninstallScriptWrite: %w", err)
	}
	err = scriptWriter.Close()
	if err != nil {
		_ = os.Remove(scriptPath)
		return fmt.Errorf("uninstallScriptClose: %w", err)
	}
	command := exec.Command(
		"powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
		"-ProcessID", fmt.Sprintf("%d", processID),
		"-RuntimePath", runtimePath,
		"-ExecutablePath", cleanExecutablePath,
		"-ScriptPath", scriptPath,
	)
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
	err = command.Start()
	if err != nil {
		_ = os.Remove(scriptPath)
		return fmt.Errorf("uninstallStart: %w", err)
	}
	err = command.Process.Release()
	if err != nil {
		return fmt.Errorf("uninstallRelease: %w", err)
	}
	return nil
}
