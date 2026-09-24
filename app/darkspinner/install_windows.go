//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

func isAutomaticRelocationSupported() bool {
	return true
}

func discoverSteamRoots() []string {
	rootSet := make(map[string]struct{})
	queries := []struct {
		root registry.Key
		path string
		name string
		view uint32
	}{
		{registry.CURRENT_USER, `Software\Valve\Steam`, "SteamPath", 0},
		{registry.LOCAL_MACHINE, `Software\Valve\Steam`, "InstallPath", registry.WOW64_64KEY},
		{registry.LOCAL_MACHINE, `Software\Valve\Steam`, "InstallPath", registry.WOW64_32KEY},
	}
	for _, query := range queries {
		key, err := registry.OpenKey(query.root, query.path, registry.QUERY_VALUE|query.view)
		if err != nil {
			continue
		}
		path, _, readErr := key.GetStringValue(query.name)
		_ = key.Close()
		if readErr == nil {
			addSteamRoot(rootSet, path)
		}
	}
	roots := make([]string, 0, len(rootSet))
	for root := range rootSet {
		roots = append(roots, root)
	}
	return roots
}

func addSteamRoot(rootSet map[string]struct{}, path string) {
	path = strings.TrimSpace(strings.ReplaceAll(path, `\\`, `\`))
	if path == "" {
		return
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return
	}
	rootSet[filepath.Clean(absolutePath)] = struct{}{}
}

func discoverSteamGameRoot(steamRoots []string) string {
	uninstallPaths := []string{
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Steam App ` + steamDemoAppID,
	}
	for _, uninstallPath := range uninstallPaths {
		for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
			for _, view := range []uint32{registry.WOW64_64KEY, registry.WOW64_32KEY} {
				key, err := registry.OpenKey(root, uninstallPath, registry.QUERY_VALUE|view)
				if err != nil {
					continue
				}
				installPath, _, readErr := key.GetStringValue("InstallLocation")
				_ = key.Close()
				if readErr == nil && isLocalGameRoot(installPath) {
					return filepath.Clean(installPath)
				}
			}
		}
	}
	return discoverSteamLibraryGameRoot(steamRoots)
}

func relocateAndRestart(sourcePath, destinationPath string, restartArguments []string) error {
	stagedPath := destinationPath + ".pending-" + strconv.Itoa(os.Getpid())
	r, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("sourceOpen: %w", err)
	}
	defer r.Close()
	w, err := os.OpenFile(stagedPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("stageCreate: %w", err)
	}
	_, copyErr := w.ReadFrom(r)
	closeErr := w.Close()
	if copyErr != nil {
		_ = os.Remove(stagedPath)
		return fmt.Errorf("stageCopy: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(stagedPath)
		return fmt.Errorf("stageClose: %w", closeErr)
	}
	err = replaceAndRestartWithArguments(stagedPath, destinationPath, restartArguments)
	if err != nil {
		_ = os.Remove(stagedPath)
		return fmt.Errorf("replaceStart: %w", err)
	}
	return nil
}

func replaceAndRestart(stagedPath, destinationPath string, restartArguments []string) error {
	return replaceAndRestartWithArguments(stagedPath, destinationPath, restartArguments)
}

func replaceAndRestartWithArguments(stagedPath, destinationPath string, restartArguments []string) error {
	scriptPath := filepath.Join(os.TempDir(), "darkspinner-relocate-"+strconv.Itoa(os.Getpid())+".ps1")
	script := `param(
    [Parameter(Mandatory=$true)][string]$StagedPath,
    [Parameter(Mandatory=$true)][string]$DestinationPath,
    [Parameter(Mandatory=$true)][int]$ParentPid,
    [string]$RestartArguments
)
$ErrorActionPreference = 'Stop'
Wait-Process -Id $ParentPid -ErrorAction SilentlyContinue
$DestinationDirectory = Split-Path -Parent $DestinationPath
Move-Item -LiteralPath $StagedPath -Destination $DestinationPath -Force
$Arguments = @()
if ($RestartArguments) {
    $Arguments = ($RestartArguments -split ',') | ForEach-Object { '--' + $_ }
}
if ($Arguments.Count -gt 0) {
    Start-Process -FilePath $DestinationPath -WorkingDirectory $DestinationDirectory -ArgumentList $Arguments
} else {
    Start-Process -FilePath $DestinationPath -WorkingDirectory $DestinationDirectory
}
Remove-Item -LiteralPath $PSCommandPath -Force
`
	err := os.WriteFile(scriptPath, []byte(script), 0o600)
	if err != nil {
		_ = os.Remove(stagedPath)
		return fmt.Errorf("scriptWrite: %w", err)
	}
	arguments := []string{
		"powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", scriptPath, "-StagedPath", stagedPath, "-DestinationPath", destinationPath,
		"-ParentPid", strconv.Itoa(os.Getpid()),
	}
	if len(restartArguments) > 0 {
		restartNames := make([]string, 0, len(restartArguments))
		for _, argument := range restartArguments {
			restartNames = append(restartNames, strings.TrimPrefix(argument, "--"))
		}
		arguments = append(arguments, "-RestartArguments", strings.Join(restartNames, ","))
	}
	command := exec.Command(arguments[0], arguments[1:]...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	err = command.Start()
	if err != nil {
		_ = os.Remove(scriptPath)
		return fmt.Errorf("scriptStart: %w", err)
	}
	return nil
}

func restartAfterExit(executablePath string, restartArguments []string) error {
	scriptPath := filepath.Join(os.TempDir(), "darkspinner-restart-"+strconv.Itoa(os.Getpid())+".ps1")
	script := `param(
    [Parameter(Mandatory=$true)][string]$ExecutablePath,
    [Parameter(Mandatory=$true)][int]$ParentPid,
    [string]$RestartArguments
)
$ErrorActionPreference = 'Stop'
Wait-Process -Id $ParentPid -ErrorAction SilentlyContinue
$WorkingDirectory = Split-Path -Parent $ExecutablePath
$Arguments = @()
if ($RestartArguments) {
    $Arguments = ($RestartArguments -split ',') | ForEach-Object { '--' + $_ }
}
if ($Arguments.Count -gt 0) {
    Start-Process -FilePath $ExecutablePath -WorkingDirectory $WorkingDirectory -ArgumentList $Arguments
} else {
    Start-Process -FilePath $ExecutablePath -WorkingDirectory $WorkingDirectory
}
Remove-Item -LiteralPath $PSCommandPath -Force
`
	err := os.WriteFile(scriptPath, []byte(script), 0o600)
	if err != nil {
		return fmt.Errorf("scriptWrite: %w", err)
	}
	arguments := []string{
		"powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", scriptPath, "-ExecutablePath", executablePath, "-ParentPid", strconv.Itoa(os.Getpid()),
	}
	if len(restartArguments) > 0 {
		restartNames := make([]string, 0, len(restartArguments))
		for _, argument := range restartArguments {
			restartNames = append(restartNames, strings.TrimPrefix(argument, "--"))
		}
		arguments = append(arguments, "-RestartArguments", strings.Join(restartNames, ","))
	}
	command := exec.Command(arguments[0], arguments[1:]...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	err = command.Start()
	if err != nil {
		_ = os.Remove(scriptPath)
		return fmt.Errorf("scriptStart: %w", err)
	}
	return nil
}
