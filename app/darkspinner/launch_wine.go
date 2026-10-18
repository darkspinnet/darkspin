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

// wineRunner is the Wine executable set and environment one launch runs in:
// either the system Wine on PATH with its default prefix, or an installed Wine
// or Proton build with its launcher-owned prefix exported as WINEPREFIX.
type wineRunner struct {
	winePath    string
	environment []string
}

func newWineRunner(wineLaunch wineLaunchEnvironment) (*wineRunner, error) {
	runner := &wineRunner{environment: os.Environ()}
	if wineLaunch.prefixPath != "" {
		runner.environment = replaceEnvironment(runner.environment, "WINEPREFIX", wineLaunch.prefixPath)
	}
	if wineLaunch.runnerPath == "" {
		winePath, err := exec.LookPath("wine")
		if err != nil {
			return nil, errors.New("wine is required to launch the game")
		}
		runner.winePath = winePath
		return runner, nil
	}
	binPath := filepath.Join(wineLaunch.runnerPath, "bin")
	runner.winePath = filepath.Join(binPath, "wine")
	fi, err := os.Stat(runner.winePath)
	if err != nil || fi.IsDir() {
		return nil, fmt.Errorf("Wine runtime is missing %s", runner.winePath)
	}
	runner.environment = prependEnvironmentPath(runner.environment, "PATH", []string{binPath})
	// Mirror the proton script: Proton 10 keeps native libraries under
	// lib/<triplet>, older builds under lib64 and lib, and the PE vkd3d
	// libraries that wined3d imports under lib/vkd3d beside lib/wine.
	libPath := filepath.Join(wineLaunch.runnerPath, "lib")
	lib64Path := filepath.Join(wineLaunch.runnerPath, "lib64")
	libraryPaths := existingDirectories(
		filepath.Join(libPath, "x86_64-linux-gnu"), filepath.Join(libPath, "i386-linux-gnu"), lib64Path, libPath,
	)
	runner.environment = prependEnvironmentPath(runner.environment, "LD_LIBRARY_PATH", libraryPaths)
	dllPaths := existingDirectories(
		filepath.Join(libPath, "vkd3d"), filepath.Join(lib64Path, "vkd3d"),
		filepath.Join(lib64Path, "wine"), filepath.Join(libPath, "wine"),
	)
	if len(dllPaths) > 0 {
		runner.environment = replaceEnvironment(runner.environment, "WINEDLLPATH", strings.Join(dllPaths, ":"))
	}
	pluginPaths := existingDirectories(
		filepath.Join(libPath, "x86_64-linux-gnu", "gstreamer-1.0"), filepath.Join(libPath, "i386-linux-gnu", "gstreamer-1.0"),
		filepath.Join(lib64Path, "gstreamer-1.0"), filepath.Join(libPath, "gstreamer-1.0"),
	)
	if len(pluginPaths) > 0 {
		runner.environment = replaceEnvironment(runner.environment, "GST_PLUGIN_SYSTEM_PATH_1_0", strings.Join(pluginPaths, ":"))
	}
	runner.environment = replaceEnvironment(runner.environment, "WINELOADER", runner.winePath)
	runner.environment = replaceEnvironment(runner.environment, "WINESERVER", filepath.Join(binPath, "wineserver"))
	if wineLaunch.isProton {
		runner.environment = replaceEnvironment(runner.environment, "WINEESYNC", "1")
		runner.environment = replaceEnvironment(runner.environment, "WINEFSYNC", "1")
	}
	return runner, nil
}

func (e *wineRunner) command(ctx context.Context, arguments ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, e.winePath, arguments...)
	command.Env = e.environment
	return command
}

// windowsPath converts a host path to the Windows form seen inside the prefix.
func (e *wineRunner) windowsPath(ctx context.Context, path string) (string, error) {
	return e.convertPath(ctx, "-w", path)
}

// unixPath converts a Windows path inside the prefix to its host location.
func (e *wineRunner) unixPath(ctx context.Context, path string) (string, error) {
	return e.convertPath(ctx, "-u", path)
}

// convertPath runs the built-in winepath program through the Wine loader, since
// Proton builds ship no winepath wrapper script in their bin directory.
func (e *wineRunner) convertPath(ctx context.Context, flag, path string) (string, error) {
	command := e.command(ctx, "winepath", flag, path)
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

func launchInjected(
	ctx context.Context, gamePath, gameWorkingDirectory, fangPath string,
	gameArguments []string, serverAddress string, wineLaunch wineLaunchEnvironment,
) error {
	runner, err := newWineRunner(wineLaunch)
	if err != nil {
		return fmt.Errorf("wineRunner: %w", err)
	}
	proxyPath := filepath.Join(filepath.Dir(gamePath), "VERSION.dll")
	cleanupProxy, err := installProxy(proxyPath, embeddedProxy)
	if err != nil {
		return fmt.Errorf("proxyPrepare: %w", err)
	}
	defer cleanupProxy()
	fangWinePath, err := runner.windowsPath(ctx, fangPath)
	if err != nil {
		return fmt.Errorf("fangWinePath: %w", err)
	}
	versionPath, err := materializeWineVersion(ctx, runner, filepath.Dir(fangPath))
	if err != nil {
		return fmt.Errorf("versionPrepare: %w", err)
	}
	versionWinePath, err := runner.windowsPath(ctx, versionPath)
	if err != nil {
		return fmt.Errorf("versionWinePath: %w", err)
	}
	arguments := []string{gamePath}
	arguments = append(arguments, gameArguments...)
	command := runner.command(ctx, arguments...)
	command.Dir = gameWorkingDirectory
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	var standardError bytes.Buffer
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
	proxyLogWinePath, err := runner.windowsPath(ctx, proxyLogPath)
	if err != nil {
		return fmt.Errorf("proxyLogWinePath: %w", err)
	}
	command.Env = replaceEnvironment(command.Env, "DARKSPIN_PROXY_LOG", proxyLogWinePath)
	tracePath := os.Getenv("DARKSPIN_CLIENT_TRACE")
	if tracePath != "" {
		traceWinePath, traceErr := runner.windowsPath(ctx, tracePath)
		if traceErr != nil {
			return fmt.Errorf("traceWinePath: %w", traceErr)
		}
		command.Env = replaceEnvironment(command.Env, "DARKSPIN_CLIENT_TRACE", traceWinePath)
	}
	snapshotControlPath := os.Getenv(snapshotControlEnvironment)
	if snapshotControlPath != "" {
		snapshotControlWinePath, controlErr := runner.windowsPath(ctx, snapshotControlPath)
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

func materializeWineVersion(ctx context.Context, runner *wineRunner, cachePath string) (string, error) {
	candidate := []string{
		`C:\windows\syswow64\version.dll`,
		`C:\windows\system32\version.dll`,
	}
	var contents []byte
	for _, windowsPath := range candidate {
		unixPath, err := runner.unixPath(ctx, windowsPath)
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

// prependEnvironmentPath puts directories ahead of a colon-separated list such
// as PATH or LD_LIBRARY_PATH, keeping the existing entries after them.
func prependEnvironmentPath(environment []string, name string, directories []string) []string {
	if len(directories) == 0 {
		return environment
	}
	content := strings.Join(directories, ":")
	prefix := name + "="
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			continue
		}
		current := strings.TrimPrefix(entry, prefix)
		if current != "" {
			content += ":" + current
		}
		break
	}
	return replaceEnvironment(environment, name, content)
}

func existingDirectories(paths ...string) []string {
	directories := make([]string, 0, len(paths))
	for _, path := range paths {
		fi, err := os.Stat(path)
		if err != nil || !fi.IsDir() {
			continue
		}
		directories = append(directories, path)
	}
	return directories
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
