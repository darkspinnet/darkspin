//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"debug/pe"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func launchInjected(
	ctx context.Context, gamePath, gameWorkingDirectory, fangPath string,
	gameArguments []string, serverAddress string,
) error {
	winePath, err := exec.LookPath("wine")
	if err != nil {
		return errors.New("wine is required to launch the game")
	}
	proxyPath := filepath.Join(filepath.Dir(gamePath), "VERSION.dll")
	cleanupProxy, err := installProxy(proxyPath, embeddedProxy)
	if err != nil {
		return fmt.Errorf("proxyPrepare: %w", err)
	}
	defer cleanupProxy()
	fangWinePath, err := resolveWinePath(ctx, fangPath)
	if err != nil {
		return fmt.Errorf("fangWinePath: %w", err)
	}
	versionPath, err := materializeWineVersion(ctx, filepath.Dir(fangPath))
	if err != nil {
		return fmt.Errorf("versionPrepare: %w", err)
	}
	versionWinePath, err := resolveWinePath(ctx, versionPath)
	if err != nil {
		return fmt.Errorf("versionWinePath: %w", err)
	}
	arguments := []string{gamePath}
	arguments = append(arguments, gameArguments...)
	command := exec.CommandContext(ctx, winePath, arguments...)
	command.Dir = gameWorkingDirectory
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	var standardError bytes.Buffer
	command.Env = os.Environ()
	command.Env = replaceEnvironment(command.Env, "DARKSPIN_FANG_DLL", fangWinePath)
	command.Env = replaceEnvironment(command.Env, "DARKSPIN_VERSION_DLL", versionWinePath)
	command.Env = replaceEnvironment(command.Env, serverAddressEnvironment, serverAddress)
	command.Env = replaceEnvironment(command.Env, "WINEDLLOVERRIDES", wineDLLOverrides(os.Getenv("WINEDLLOVERRIDES")))
	proxyLogPath := filepath.Join(filepath.Dir(filepath.Dir(fangPath)), "logs", "fangproxy.log")
	err = os.MkdirAll(filepath.Dir(proxyLogPath), 0o755)
	if err != nil {
		return fmt.Errorf("proxyLogMkdir: %w", err)
	}
	err = os.WriteFile(proxyLogPath, nil, 0o644)
	if err != nil {
		return fmt.Errorf("proxyLogReset: %w", err)
	}
	wineLogPath := filepath.Join(filepath.Dir(proxyLogPath), "wine.log")
	wineLog, err := os.Create(wineLogPath)
	if err != nil {
		return fmt.Errorf("wineLogCreate: %w", err)
	}
	defer wineLog.Close()
	command.Stderr = io.MultiWriter(&standardError, wineLog)
	proxyLogWinePath, err := resolveWinePath(ctx, proxyLogPath)
	if err != nil {
		return fmt.Errorf("proxyLogWinePath: %w", err)
	}
	command.Env = replaceEnvironment(command.Env, "DARKSPIN_PROXY_LOG", proxyLogWinePath)
	tracePath := os.Getenv("DARKSPIN_CLIENT_TRACE")
	if tracePath != "" {
		traceWinePath, traceErr := resolveWinePath(ctx, tracePath)
		if traceErr != nil {
			return fmt.Errorf("traceWinePath: %w", traceErr)
		}
		command.Env = replaceEnvironment(command.Env, "DARKSPIN_CLIENT_TRACE", traceWinePath)
	}
	snapshotControlPath := os.Getenv(snapshotControlEnvironment)
	if snapshotControlPath != "" {
		snapshotControlWinePath, controlErr := resolveWinePath(ctx, snapshotControlPath)
		if controlErr != nil {
			return fmt.Errorf("snapshotControlWinePath: %w", controlErr)
		}
		command.Env = replaceEnvironment(
			command.Env, snapshotControlEnvironment, snapshotControlWinePath,
		)
	}
	err = command.Run()
	if err != nil {
		diagnostic := strings.TrimSpace(standardError.String())
		if diagnostic != "" {
			return fmt.Errorf("wineRun: %w: %s", err, diagnostic)
		}
		return fmt.Errorf("wineRun: %w", err)
	}
	return nil
}

func materializeWineVersion(ctx context.Context, cachePath string) (string, error) {
	candidate := []string{
		`C:\windows\syswow64\version.dll`,
		`C:\windows\system32\version.dll`,
	}
	var contents []byte
	for _, windowsPath := range candidate {
		unixPath, err := resolveWineUnixPath(ctx, windowsPath)
		if err != nil {
			continue
		}
		candidateContents, err := os.ReadFile(unixPath)
		if err != nil || !isPE32DLL(candidateContents) {
			continue
		}
		contents = candidateContents
		break
	}
	if len(contents) == 0 {
		return "", errors.New("Wine 32-bit version.dll was not found")
	}
	err := os.MkdirAll(cachePath, 0o755)
	if err != nil {
		return "", fmt.Errorf("cacheMkdir: %w", err)
	}
	path := filepath.Join(cachePath, "wine-version.dll")
	err = os.WriteFile(path, contents, 0o600)
	if err != nil {
		return "", fmt.Errorf("versionWrite: %w", err)
	}
	return path, nil
}

func resolveWineUnixPath(ctx context.Context, path string) (string, error) {
	winePath, err := exec.LookPath("winepath")
	if err != nil {
		return "", errors.New("winepath is required to launch the game")
	}
	command := exec.CommandContext(ctx, winePath, "-u", path)
	contents, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("winepathRun: %w", err)
	}
	resolvedPath := strings.TrimSpace(string(contents))
	if resolvedPath == "" {
		return "", errors.New("winepath returned an empty path")
	}
	return resolvedPath, nil
}

func isPE32DLL(contents []byte) bool {
	executable, err := pe.NewFile(bytes.NewReader(contents))
	if err != nil {
		return false
	}
	defer executable.Close()
	return executable.FileHeader.Machine == pe.IMAGE_FILE_MACHINE_I386
}

func wineDLLOverrides(current string) string {
	current = strings.TrimSpace(current)
	if current == "" {
		return "d3d9=b;version=n,b"
	}
	return "d3d9=b;version=n,b;" + current
}

func installProxy(path string, payload []byte) (func(), error) {
	if len(payload) == 0 {
		return nil, errors.New("Wine startup proxy is not embedded in this development build")
	}
	contents, err := os.ReadFile(path)
	if err == nil && bytes.Equal(contents, payload) {
		return func() { _ = os.Remove(path) }, nil
	}
	if err == nil {
		if !bytes.Contains(contents, []byte(proxyMarker)) {
			return nil, fmt.Errorf("%s already exists and is not the DarkSpinner proxy", path)
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("proxyRead: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return nil, fmt.Errorf("proxyMkdir: %w", err)
	}
	err = os.WriteFile(path, payload, 0o600)
	if err != nil {
		return nil, fmt.Errorf("proxyWrite: %w", err)
	}
	return func() { _ = os.Remove(path) }, nil
}

func preparePlatformGame(pathSet *spinnerPathSet) error {
	proxyPath := filepath.Join(filepath.Dir(pathSet.gameBinaryPath), "VERSION.dll")
	contents, err := os.ReadFile(proxyPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("proxyRead: %w", err)
	}
	if !bytes.Contains(contents, []byte(proxyMarker)) {
		return nil
	}
	err = os.Remove(proxyPath)
	if err != nil {
		return fmt.Errorf("proxyRemove: %w", err)
	}
	return nil
}

func resolveWinePath(ctx context.Context, path string) (string, error) {
	winePath, err := exec.LookPath("winepath")
	if err != nil {
		return "", errors.New("winepath is required to launch the game")
	}
	command := exec.CommandContext(ctx, winePath, "-w", path)
	contents, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("winepathRun: %w", err)
	}
	resolvedPath := strings.TrimSpace(string(contents))
	if resolvedPath == "" {
		return "", errors.New("winepath returned an empty path")
	}
	return resolvedPath, nil
}

func replaceEnvironment(environment []string, name, content string) []string {
	prefix := name + "="
	updatedEnvironment := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		updatedEnvironment = append(updatedEnvironment, entry)
	}
	return append(updatedEnvironment, prefix+content)
}
