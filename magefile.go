//go:build mage

package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

// releaseSemver is the single source of truth for Dark Spin release versions.
// Build targets inject it without rewriting application or frontend sources.
const releaseSemver = "1.0.1"

const binaryName = "darkrun.exe"

const darkSpinBinaryName = "darkspin.exe"

const darkSpinnerBinaryName = "darkspinner.exe"

const darkSpinnerLinuxBinaryName = "darkspinner"

const darkSpinnerHelperBinaryName = "darkspinner-helper.exe"

const darkSpinnerProxyBinaryName = "fangproxy.dll"

const darkSpinnerSteamProxyBinaryName = "steamproxy.dll"

const wasmAuthBinaryName = "wasmauth.wasm"

const darkSpinVersionVariable = "main.Version"

const darkSpinAuthURLVariable = "github.com/darkspinnet/darkspin/app/darkspin.authServiceURL"

const darkSpinPatchURLVariable = "github.com/darkspinnet/darkspin/app/darkspin.patchManifestURL"

const darkSpinnerAuthURLVariable = "github.com/darkspinnet/darkspin/app/darkspinner.authServiceURL"

const darkSpinnerPatchURLVariable = "github.com/darkspinnet/darkspin/app/darkspinner.patchManifestURL"

const darkSpinnerUpdateURLVariable = "main.launcherUpdateManifestURL"

const darkSpinnerVersionVariable = "main.Version"

const darkSpinnerServiceVersionVariable = "github.com/darkspinnet/darkspin/server/buildinfo.Version"

const primaryLocalAccount = "darkrun"

const secondaryLocalAccount = "darkrun2"

// Aliases exposes target names that are clearer at the command line.
var Aliases = map[string]interface{}{
	"build_wasmauth": BuildWasmAuth,
}

var semanticVersionPattern = regexp.MustCompile(
	`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`,
)

// Version validates the version used by build-time injection.
func Version() error {
	version := buildVersion()
	if !semanticVersionPattern.MatchString(version) {
		return fmt.Errorf("buildVersion: %q is not semantic versioning", version)
	}
	return nil
}

func buildVersion() string {
	version := strings.TrimSpace(os.Getenv("DARKSPIN_BUILD_VERSION"))
	if version != "" {
		return version
	}
	return releaseSemver
}

// Darkrun groups standalone server commands.
type Darkrun mg.Namespace

// Darkspinner groups all-in-one desktop application commands.
type Darkspinner mg.Namespace

// Darkspin groups generic launcher commands.
type Darkspin mg.Namespace

// Tutorial groups local profile and traced development workflows.
type Tutorial mg.Namespace

// Build compiles Darkrun, DarkSpinner, the generic launcher, and Fang.
func Build() error {
	mg.SerialDeps(Version)

	err := os.MkdirAll("bin", 0o755)
	if err != nil {
		return fmt.Errorf("binMkdir: %w", err)
	}
	err = os.MkdirAll(filepath.Join("bin", "game"), 0o755)
	if err != nil {
		return fmt.Errorf("gameMkdir: %w", err)
	}

	legacyLauncherPath := filepath.Join("bin", "launcher.exe")
	err = os.Remove(legacyLauncherPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("legacyRemove: %w", err)
	}

	linkerFlags, err := buildDarkrun()
	if err != nil {
		return fmt.Errorf("serverBuild: %w", err)
	}

	darkSpinLinkerFlags, err := launcherLinkerFlags(linkerFlags)
	if err != nil {
		return fmt.Errorf("launcherFlags: %w", err)
	}
	err = buildLauncher(darkSpinLinkerFlags)
	if err != nil {
		return fmt.Errorf("launcherBuild: %w", err)
	}
	err = buildFang(true)
	if err != nil {
		return fmt.Errorf("fangBuild: %w", err)
	}
	spinnerLinkerFlags, err := darkSpinnerLinkerFlags(linkerFlags)
	if err != nil {
		return fmt.Errorf("darkSpinnerFlags: %w", err)
	}
	err = prepareDarkSpinnerFrontend()
	if err != nil {
		return fmt.Errorf("darkSpinnerFrontend: %w", err)
	}
	err = buildDarkSpinner(spinnerLinkerFlags, filepath.Join("bin", "game"), runtime.GOOS)
	if err != nil {
		return fmt.Errorf("darkSpinnerBuild: %w", err)
	}

	return nil
}

func buildDarkrun() (string, error) {
	outputDirectory := filepath.Join("bin", "server")
	err := os.MkdirAll(outputDirectory, 0o755)
	if err != nil {
		return "", fmt.Errorf("outputMkdir: %w", err)
	}
	outputPath := filepath.Join(outputDirectory, binaryName)
	linkerFlags := buildVersionLinkerFlags()
	err = sh.RunV("go", "build", "-ldflags", linkerFlags, "-o", outputPath, "./app/darkrun")
	if err != nil {
		return "", fmt.Errorf("goBuild: %w", err)
	}
	return linkerFlags, nil
}

func buildVersionLinkerFlags() string {
	buildID := time.Now().UTC().Format("20060102T150405.000000000Z")
	return "-X github.com/darkspinnet/darkspin/server/buildinfo.ID=" + buildID +
		" -X " + darkSpinnerServiceVersionVariable + "=" + buildVersion()
}

func launcherLinkerFlags(base string) (string, error) {
	flags, err := desktopLinkerFlags(base, darkSpinAuthURLVariable, darkSpinPatchURLVariable)
	if err != nil {
		return "", fmt.Errorf("desktopFlags: %w", err)
	}
	return flags + " -X " + darkSpinVersionVariable + "=" + buildVersion(), nil
}

func desktopLinkerFlags(base, authVariable, patchVariable string) (string, error) {
	authURL := strings.TrimSpace(os.Getenv("DARKRUN_AUTH_URL"))
	if authURL != "" && strings.ContainsAny(authURL, " \t\r\n") {
		return "", errors.New("DARKRUN_AUTH_URL cannot contain whitespace")
	}
	patchURL := strings.TrimSpace(os.Getenv("DARKRUN_PATCH_URL"))
	if patchURL != "" && strings.ContainsAny(patchURL, " \t\r\n") {
		return "", errors.New("DARKRUN_PATCH_URL cannot contain whitespace")
	}
	flags := base
	if authURL != "" {
		flags += " -X " + authVariable + "=" + authURL
	}
	if patchURL != "" {
		flags += " -X " + patchVariable + "=" + patchURL
	}
	return flags, nil
}

func darkSpinnerLinkerFlags(base string) (string, error) {
	flags, err := desktopLinkerFlags(base, darkSpinnerAuthURLVariable, darkSpinnerPatchURLVariable)
	if err != nil {
		return "", fmt.Errorf("desktopFlags: %w", err)
	}
	updateURL := strings.TrimSpace(os.Getenv("DARKSPINNER_UPDATE_URL"))
	if updateURL == "" && darkSpinnerArchitecture("windows") == "amd64" {
		updateURL = "https://github.com/darkspinnet/darkspin/releases/latest/download/darkspinner-update-windows-amd64.json"
	}
	if updateURL != "" && strings.ContainsAny(updateURL, " \t\r\n") {
		return "", errors.New("DARKSPINNER_UPDATE_URL cannot contain whitespace")
	}
	if updateURL != "" {
		flags += " -X " + darkSpinnerUpdateURLVariable + "=" + updateURL
	}
	return flags + " -X " + darkSpinnerVersionVariable + "=" + buildVersion(), nil
}

func buildFang(isDiagnostics bool) error {
	fangPath := filepath.Join("bin", "game", "fang.dll")
	environment := map[string]string{"GOOS": "windows", "GOARCH": "386", "CGO_ENABLED": "1"}
	arguments := []string{"build", "-ldflags", "-s -w", "-buildmode=c-shared", "-o", fangPath}
	if isDiagnostics {
		arguments = append(arguments, "-tags", "fangdebug")
	}
	arguments = append(arguments, "./app/fang")
	if runtime.GOOS != "windows" {
		compiler := "i686-w64-mingw32-gcc"
		_, err := exec.LookPath(compiler)
		if err != nil {
			return fmt.Errorf("compilerLookup: %s is required to cross-compile fang.dll", compiler)
		}
		environment["CC"] = compiler
	}
	err := sh.RunWithV(
		environment,
		"go", arguments...,
	)
	if err != nil {
		return fmt.Errorf("fangCompile: %w", err)
	}
	generatedHeaderPath := filepath.Join("bin", "game", "fang.h")
	err = os.Remove(generatedHeaderPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("headerRemove: %w", err)
	}
	return nil
}

func buildLauncher(linkerFlags string) error {
	launcherDirectory := filepath.Join("app", "darkspin")
	_, err := exec.LookPath("pnpm")
	if err != nil {
		return errors.New("pnpm is required to build darkspin.exe; install pnpm and ensure it is available in PATH")
	}
	frontendDirectory := filepath.Join(launcherDirectory, "frontend")
	environmentVariables := map[string]string{"CI": "true"}
	pnpmStorePath := filepath.Join(os.TempDir(), "darkspin-pnpm-store")
	err = runCommandWithEnvironment(frontendDirectory, environmentVariables, "pnpm", "--store-dir", pnpmStorePath, "install", "--frozen-lockfile")
	if err != nil {
		return fmt.Errorf("frontendInstall: %w", err)
	}
	err = runCommandWithEnvironment(frontendDirectory, environmentVariables, "pnpm", "run", "build")
	if err != nil {
		return fmt.Errorf("frontendBuild: %w", err)
	}
	arguments := []string{
		"build", "-clean", "-s", "-platform", "windows/386",
		"-o", darkSpinBinaryName, "-m", "-nosyncgomod", "-tags", "frontend",
		"-ldflags", linkerFlags,
	}
	arguments = appendCrossBuildArguments(arguments, "windows")
	err = runVersionedWailsBuild(launcherDirectory, arguments...)
	if err != nil {
		return fmt.Errorf("wailsBuild: %w", err)
	}
	source := filepath.Join(launcherDirectory, "build", "bin", darkSpinBinaryName)
	target := filepath.Join("bin", "game", darkSpinBinaryName)
	err = copyFile(source, target)
	if err != nil {
		return fmt.Errorf("launcherCopy: %w", err)
	}
	return nil
}

func buildDarkSpinner(linkerFlags, outputPath, targetOS string) error {
	targetArch := darkSpinnerArchitecture(targetOS)
	if targetOS == "windows" && targetArch == "amd64" {
		helperPath := filepath.Join("app", "darkspinner", "fangloader.exe")
		environment := map[string]string{"GOOS": "windows", "GOARCH": "386", "CGO_ENABLED": "0"}
		err := runCommandWithEnvironment("", environment, "go", "build", "-trimpath",
			"-ldflags", "-s -w", "-o", helperPath, "./app/fangloader")
		if err != nil {
			return fmt.Errorf("loaderBuild: %w", err)
		}
		defer removeDarkSpinnerLoader(helperPath)
	}

	projectPath := filepath.Join("app", "darkspinner")
	embeddedFangPath := filepath.Join(projectPath, "fang.dll")
	err := copyFile(filepath.Join("bin", "game", "fang.dll"), embeddedFangPath)
	if err != nil {
		return fmt.Errorf("fangEmbed: %w", err)
	}
	embeddedProxyPath := ""
	embeddedSteamProxyPath := ""
	if targetOS != "windows" {
		embeddedProxyPath = filepath.Join(projectPath, darkSpinnerProxyBinaryName)
		err = buildFangProxy(embeddedProxyPath)
		if err != nil {
			_ = os.Remove(embeddedFangPath)
			return fmt.Errorf("proxyBuild: %w", err)
		}
	} else {
		embeddedSteamProxyPath = filepath.Join(projectPath, darkSpinnerSteamProxyBinaryName)
		err = buildFangProxy(embeddedSteamProxyPath)
		if err != nil {
			_ = os.Remove(embeddedFangPath)
			return fmt.Errorf("steamProxyBuild: %w", err)
		}
	}
	platform := targetOS + "/" + targetArch
	outputName := darkSpinnerBinaryName
	buildTags := "frontend,fang"
	if targetOS != "windows" {
		outputName = darkSpinnerLinuxBinaryName
		buildTags += ",proxy"
		if targetOS == "linux" {
			buildTags += ",webkit2_41"
		}
	} else {
		buildTags += ",steamproxy"
		if targetArch == "amd64" {
			buildTags += ",loader"
		}
	}
	arguments := []string{
		"build", "-clean", "-s", "-platform", platform,
		"-o", outputName, "-m", "-nosyncgomod", "-tags", buildTags,
		"-ldflags", linkerFlags,
	}
	arguments = appendCrossBuildArguments(arguments, targetOS)
	buildErr := runVersionedWailsBuild(projectPath, arguments...)
	removeFangErr := os.Remove(embeddedFangPath)
	var removeProxyErr error
	if embeddedProxyPath != "" {
		removeProxyErr = os.Remove(embeddedProxyPath)
	}
	if embeddedSteamProxyPath != "" {
		steamProxyErr := os.Remove(embeddedSteamProxyPath)
		if removeProxyErr == nil {
			removeProxyErr = steamProxyErr
		}
	}
	if buildErr != nil {
		return fmt.Errorf("wailsBuild: %w", buildErr)
	}
	if removeFangErr != nil {
		return fmt.Errorf("fangCleanup: %w", removeFangErr)
	}
	if removeProxyErr != nil {
		return fmt.Errorf("proxyCleanup: %w", removeProxyErr)
	}
	err = os.MkdirAll(outputPath, 0o755)
	if err != nil {
		return fmt.Errorf("outputMkdir: %w", err)
	}
	sourcePath := filepath.Join(projectPath, "build", "bin", outputName)
	if targetOS == "darwin" {
		outputName = "darkspinner.app"
		sourcePath = filepath.Join(projectPath, "build", "bin", outputName)
		err = runCommand("", "ditto", sourcePath, filepath.Join(outputPath, outputName))
	} else {
		err = copyFile(sourcePath, filepath.Join(outputPath, outputName))
	}
	if err != nil {
		return fmt.Errorf("binaryCopy: %w", err)
	}
	if targetOS == "linux" {
		legacyHelperPath := filepath.Join(outputPath, darkSpinnerHelperBinaryName)
		err = os.Remove(legacyHelperPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("legacyHelperRemove: %w", err)
		}
	}
	err = prepareDarkSpinnerRuntime(outputPath)
	if err != nil {
		return fmt.Errorf("runtimeCopy: %w", err)
	}
	return nil
}

func prepareDarkSpinnerFrontend() error {
	projectPath := filepath.Join("app", "darkspinner")
	_, err := exec.LookPath("pnpm")
	if err != nil {
		return errors.New("pnpm is required to build darkspinner.exe; install pnpm and ensure it is available in PATH")
	}
	frontendPath := filepath.Join(projectPath, "frontend")
	publicPath := filepath.Join(frontendPath, "public")
	err = os.MkdirAll(publicPath, 0o755)
	if err != nil {
		return fmt.Errorf("changelogMkdir: %w", err)
	}
	err = copyFile("CHANGELOG.md", filepath.Join(publicPath, "changelog.md"))
	if err != nil {
		return fmt.Errorf("changelogCopy: %w", err)
	}
	environment := map[string]string{"CI": "true"}
	pnpmStorePath := filepath.Join(os.TempDir(), "darkspin-pnpm-store")
	err = runCommandWithEnvironment(frontendPath, environment, "pnpm", "--store-dir", pnpmStorePath, "install", "--frozen-lockfile")
	if err != nil {
		return fmt.Errorf("frontendInstall: %w", err)
	}
	err = runCommandWithEnvironment(frontendPath, environment, "pnpm", "run", "build")
	if err != nil {
		return fmt.Errorf("frontendBuild: %w", err)
	}
	return nil
}

func appendCrossBuildArguments(arguments []string, targetOS string) []string {
	if runtime.GOOS == "windows" && targetOS == "windows" {
		return arguments
	}
	return append(arguments, "-skipbindings")
}

func prepareDarkSpinnerRuntime(outputPath string) error {
	configPath := filepath.Join(outputPath, "darkspin.toml")
	_, err := os.Stat(configPath)
	if errors.Is(err, os.ErrNotExist) {
		err = copyFile(filepath.Join("server", "assets", "darkspin.toml"), configPath)
		if err != nil {
			return fmt.Errorf("configCopy: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("configStat: %w", err)
	}
	directories := []string{
		filepath.Join("darkspin", "logs", "traces"),
		filepath.Join("darkspin", "saves"),
		filepath.Join("darkspin", "cache"),
	}
	for index, directory := range directories {
		err = os.MkdirAll(filepath.Join(outputPath, directory), 0o755)
		if err != nil {
			return fmt.Errorf("runtimeMkdir[%d]: %w", index, err)
		}
	}
	return nil
}

func runCommand(directory, name string, arguments ...string) error {
	err := runCommandWithEnvironment(directory, nil, name, arguments...)
	if err != nil {
		return fmt.Errorf("commandRun: %w", err)
	}
	return nil
}

func runCommandWithEnvironment(directory string, environmentVariables map[string]string, name string, arguments ...string) error {
	commandDirectory, err := filepath.Abs(directory)
	if err != nil {
		return fmt.Errorf("commandPath: %w", err)
	}
	command := exec.Command(name, arguments...)
	command.Dir = commandDirectory
	command.Env = os.Environ()
	for key, environmentEntry := range environmentVariables {
		command.Env = append(command.Env, key+"="+environmentEntry)
	}
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err = command.Run()
	if err != nil {
		return fmt.Errorf("processRun: %w", err)
	}
	return nil
}

func copyFile(source, target string) error {
	r, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("copyOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("copyStat: %w", err)
	}
	w, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("copyCreate: %w", err)
	}
	_, copyErr := io.Copy(w, r)
	closeErr := w.Close()
	if copyErr != nil {
		return fmt.Errorf("copyWrite: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("copyClose: %w", closeErr)
	}
	err = os.Chmod(target, fi.Mode().Perm())
	if err != nil {
		return fmt.Errorf("copyMode: %w", err)
	}
	return nil
}

// BuildWasmAuth compiles the storage-independent auth bridge to WebAssembly.
func BuildWasmAuth() error {
	outputDirectory := filepath.Join("bin", "wasmauth")
	err := os.MkdirAll(outputDirectory, 0o755)
	if err != nil {
		return fmt.Errorf("wasmAuthMkdir: %w", err)
	}

	outputPath := filepath.Join(outputDirectory, wasmAuthBinaryName)
	environment := map[string]string{
		"GOOS":        "js",
		"GOARCH":      "wasm",
		"CGO_ENABLED": "0",
	}
	err = sh.RunWithV(environment, "go", "build", "-o", outputPath, "./app/palp")
	if err != nil {
		return fmt.Errorf("wasmAuthBuild: %w", err)
	}
	return nil
}

// Build builds the standalone Darkrun server binary.
func (Darkrun) Build() error {
	mg.SerialDeps(Version)

	_, err := buildDarkrun()
	if err != nil {
		return fmt.Errorf("darkrunBuild: %w", err)
	}
	return nil
}

// BuildCI packages the standalone amd64 server for Windows or Linux.
func (Darkrun) BuildCI(targetOS string) error {
	if targetOS != "windows" && targetOS != "linux" {
		return fmt.Errorf("unsupported Darkrun target %q", targetOS)
	}
	mg.SerialDeps(Version)
	platformName := targetOS + "-amd64"
	outputPath := filepath.Join("bin", "darkrunci", platformName)
	err := os.MkdirAll(outputPath, 0o755)
	if err != nil {
		return fmt.Errorf("outputMkdir: %w", err)
	}
	executableName := "darkrun"
	if targetOS == "windows" {
		executableName += ".exe"
	}
	executablePath := filepath.Join(outputPath, executableName)
	environment := map[string]string{"GOOS": targetOS, "GOARCH": "amd64", "CGO_ENABLED": "0"}
	err = runCommandWithEnvironment("", environment, "go", "build", "-trimpath",
		"-ldflags", "-s -w "+buildVersionLinkerFlags(), "-o", executablePath, "./app/darkrun")
	if err != nil {
		return fmt.Errorf("serverBuild: %w", err)
	}
	// Cross-compilation on Windows must still preserve Unix execute permission.
	err = os.Chmod(executablePath, 0o755)
	if err != nil {
		return fmt.Errorf("binaryMode: %w", err)
	}
	archiveName := "darkrun-" + platformName + "-v" + buildVersion() + ".zip"
	err = archiveBinary(outputPath, executableName, archiveName)
	if err != nil {
		return fmt.Errorf("serverArchive: %w", err)
	}
	return nil
}

// Build builds the all-in-one desktop runtime beside the assets under bin/game.
func (Darkspinner) Build() error {
	mg.SerialDeps(Version)

	err := buildDarkSpinnerTarget(true, true, runtime.GOOS)
	if err != nil {
		return fmt.Errorf("darkSpinnerBuild: %w", err)
	}
	return nil
}

// BuildRun builds and then launches the all-in-one desktop runtime.
func (e Darkspinner) BuildRun() error {
	err := e.Build()
	if err != nil {
		return fmt.Errorf("darkSpinnerBuildRunBuild: %w", err)
	}
	err = flushDarkSpinnerLogs()
	if err != nil {
		return fmt.Errorf("darkSpinnerBuildRunLogs: %w", err)
	}
	err = e.Run()
	if err != nil {
		return fmt.Errorf("darkSpinnerBuildRunLaunch: %w", err)
	}
	return nil
}

func flushDarkSpinnerLogs() error {
	logDirectory := filepath.Join("bin", "game", "darkspin", "logs")
	err := os.RemoveAll(logDirectory)
	if err != nil {
		return fmt.Errorf("logRemove: %w", err)
	}
	err = os.MkdirAll(filepath.Join(logDirectory, "traces"), 0o755)
	if err != nil {
		return fmt.Errorf("traceMkdir: %w", err)
	}
	return nil
}

func removeDarkSpinnerLoader(path string) {
	err := os.Remove(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "loader cleanup: %v\n", err)
	}
}

// BuildCI builds the selected native CI target.
func (Darkspinner) BuildCI() error {
	err := (Darkspinner{}).BuildCINative()
	if err != nil {
		return fmt.Errorf("nativeBuild: %w", err)
	}
	return nil
}

// CI selects the launcher architecture independently; Fang always remains x86.
func darkSpinnerArchitecture(targetOS string) string {
	targetArch := strings.TrimSpace(os.Getenv("DARKSPINNER_GOARCH"))
	if targetArch != "" {
		return targetArch
	}
	if targetOS == "windows" {
		return "386"
	}
	return runtime.GOARCH
}

// BuildCINative builds and archives a launcher on its native operating system.
func (Darkspinner) BuildCINative() error {
	mg.SerialDeps(Version)
	targetOS := runtime.GOOS
	targetArch := darkSpinnerArchitecture(targetOS)
	platformName := targetOS + "-" + targetArch
	binaryName := darkSpinnerLinuxBinaryName
	switch platformName {
	case "windows-386":
		platformName = "windows-win32"
		binaryName = darkSpinnerBinaryName
	case "windows-amd64":
		binaryName = darkSpinnerBinaryName
	case "darwin-amd64", "darwin-arm64":
		binaryName = "darkspinner.app"
	case "linux-amd64", "linux-arm64":
	default:
		return fmt.Errorf("unsupported launcher platform %s", platformName)
	}
	err := buildDarkSpinnerTarget(false, true, targetOS)
	if err != nil {
		return fmt.Errorf("nativeBuild: %w", err)
	}
	outputPath := filepath.Join("bin", "darkspinnerci")
	err = archiveDarkSpinnerBinary(outputPath, binaryName, platformName)
	if err != nil {
		return fmt.Errorf("nativeArchive: %w", err)
	}
	return nil
}

func archiveDarkSpinnerBinary(outputPath, binaryName, platformName string) error {
	binaryPath := filepath.Join(outputPath, binaryName)
	if strings.HasSuffix(binaryName, ".app") {
		archivePath := filepath.Join(outputPath, "darkspinner-"+platformName+"-v"+buildVersion()+".zip")
		err := runCommand("", "ditto", "-c", "-k", "--keepParent", binaryPath, archivePath)
		if err != nil {
			return fmt.Errorf("bundleArchive: %w", err)
		}
		return nil
	}
	archiveName := "darkspinner-v" + buildVersion() + ".zip"
	if platformName != "" {
		archiveName = "darkspinner-" + platformName + "-v" + buildVersion() + ".zip"
	}
	err := archiveBinary(outputPath, binaryName, archiveName)
	if err != nil {
		return fmt.Errorf("binaryArchive: %w", err)
	}
	return nil
}

func archiveBinary(outputPath, binaryName, archiveName string) error {
	binaryPath := filepath.Join(outputPath, binaryName)
	r, err := os.Open(binaryPath)
	if err != nil {
		return fmt.Errorf("archiveOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("archiveStat: %w", err)
	}
	archivePath := filepath.Join(outputPath, archiveName)
	w, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("archiveCreate: %w", err)
	}
	archive := zip.NewWriter(w)
	header := &zip.FileHeader{Name: binaryName, Method: zip.Deflate}
	header.SetMode(0o755)
	header.SetModTime(fi.ModTime())
	entry, err := archive.CreateHeader(header)
	if err != nil {
		_ = archive.Close()
		_ = w.Close()
		_ = os.Remove(archivePath)
		return fmt.Errorf("archiveHeader: %w", err)
	}
	_, copyErr := io.Copy(entry, r)
	archiveCloseErr := archive.Close()
	outputCloseErr := w.Close()
	if copyErr != nil {
		_ = os.Remove(archivePath)
		return fmt.Errorf("archiveWrite: %w", copyErr)
	}
	if archiveCloseErr != nil {
		_ = os.Remove(archivePath)
		return fmt.Errorf("archiveFinalize: %w", archiveCloseErr)
	}
	if outputCloseErr != nil {
		_ = os.Remove(archivePath)
		return fmt.Errorf("archiveClose: %w", outputCloseErr)
	}
	return nil
}

func buildDarkSpinnerTarget(isDevelopment bool, isFangDiagnostics bool, targetPlatforms ...string) error {
	err := os.MkdirAll(filepath.Join("bin", "game"), 0o755)
	if err != nil {
		return fmt.Errorf("gameMkdir: %w", err)
	}
	err = buildFang(isFangDiagnostics)
	if err != nil {
		return fmt.Errorf("fangBuild: %w", err)
	}
	linkerFlags := buildVersionLinkerFlags()
	if !isDevelopment {
		linkerFlags = "-s -w " + linkerFlags
	}
	linkerFlags, err = darkSpinnerLinkerFlags(linkerFlags)
	if err != nil {
		return fmt.Errorf("darkSpinnerFlags: %w", err)
	}
	err = prepareDarkSpinnerFrontend()
	if err != nil {
		return fmt.Errorf("darkSpinnerFrontend: %w", err)
	}
	outputPath := filepath.Join("bin", "game")
	if !isDevelopment {
		outputPath = filepath.Join("bin", "darkspinnerci")
	}
	for index, targetOS := range targetPlatforms {
		err = buildDarkSpinner(linkerFlags, outputPath, targetOS)
		if err != nil {
			return fmt.Errorf("darkSpinnerBuild[%d]: %w", index, err)
		}
	}
	return nil
}

func runVersionedWailsBuild(projectPath string, arguments ...string) error {
	configurationPath := filepath.Join(projectPath, "wails.json")
	contents, err := os.ReadFile(configurationPath)
	if err != nil {
		return fmt.Errorf("configRead: %w", err)
	}
	fi, err := os.Stat(configurationPath)
	if err != nil {
		return fmt.Errorf("configStat: %w", err)
	}
	projectFields := make(map[string]any)
	err = json.Unmarshal(contents, &projectFields)
	if err != nil {
		return fmt.Errorf("configDecode: %w", err)
	}
	infoFields := make(map[string]any)
	configuredInfo, isFound := projectFields["info"]
	if isFound {
		decodedInfo, isMap := configuredInfo.(map[string]any)
		if !isMap {
			return errors.New("config info must be an object")
		}
		infoFields = decodedInfo
	}
	productVersion, err := windowsProductVersion()
	if err != nil {
		return fmt.Errorf("productVersion: %w", err)
	}
	infoFields["productVersion"] = productVersion
	projectFields["info"] = infoFields
	configuredContents, err := json.Marshal(projectFields)
	if err != nil {
		return fmt.Errorf("configEncode: %w", err)
	}
	err = os.WriteFile(configurationPath, configuredContents, fi.Mode())
	if err != nil {
		return fmt.Errorf("configWrite: %w", err)
	}
	buildErr := runCommand(projectPath, "wails", arguments...)
	restoreErr := os.WriteFile(configurationPath, contents, fi.Mode())
	if buildErr != nil && restoreErr != nil {
		return errors.Join(
			fmt.Errorf("commandRun: %w", buildErr),
			fmt.Errorf("configRestore: %w", restoreErr),
		)
	}
	if restoreErr != nil {
		return fmt.Errorf("configRestore: %w", restoreErr)
	}
	if buildErr != nil {
		return fmt.Errorf("commandRun: %w", buildErr)
	}
	return nil
}

func windowsProductVersion() (string, error) {
	version := buildVersion()
	fields := semanticVersionPattern.FindStringSubmatch(version)
	if len(fields) < 4 {
		return "", fmt.Errorf("buildVersion: %q is not semantic versioning", version)
	}
	return strings.Join(fields[1:4], "."), nil
}

func buildFangProxy(outputPath string) error {
	compiler := "i686-w64-mingw32-gcc"
	_, err := exec.LookPath(compiler)
	if err != nil {
		return fmt.Errorf("compilerLookup: %s is required to build the version proxy", compiler)
	}
	err = sh.RunV(compiler, "-shared", "-static-libgcc", "-Wl,--kill-at", "-o", outputPath, filepath.Join("app", "fangproxy", "proxy.c"))
	if err != nil {
		return fmt.Errorf("compilerRun: %w", err)
	}
	return nil
}

// Run launches the existing all-in-one desktop runtime.
func (Darkspinner) Run() error {
	binaryName := darkSpinnerBinaryName
	if runtime.GOOS != "windows" {
		binaryName = darkSpinnerLinuxBinaryName
	}
	if runtime.GOOS == "darwin" {
		binaryName = filepath.Join("darkspinner.app", "Contents", "MacOS", "darkspinner")
	}
	binaryPath, err := filepath.Abs(filepath.Join("bin", "game", binaryName))
	if err != nil {
		return fmt.Errorf("darkSpinnerPath: %w", err)
	}
	command := exec.Command(binaryPath, "--skip-cinematic")
	command.Dir = filepath.Dir(binaryPath)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err = command.Run()
	if err != nil {
		return fmt.Errorf("darkSpinnerRun: %w", err)
	}
	return nil
}

// Auth rebuilds Darkrun and starts the loopback-only local authentication broker.
func (Darkrun) Auth() error {
	mg.SerialDeps(Version)

	_, err := buildDarkrun()
	if err != nil {
		return fmt.Errorf("authBuild: %w", err)
	}
	binaryPath, err := filepath.Abs(filepath.Join("bin", "server", binaryName))
	if err != nil {
		return fmt.Errorf("authPath: %w", err)
	}

	command := exec.Command(binaryPath, "auth", "--local")
	command.Dir = filepath.Dir(binaryPath)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err = command.Run()
	if err != nil {
		return fmt.Errorf("authRun: %w", err)
	}
	return nil
}

// Server rebuilds Darkrun and starts the server from the bin directory.
func (Darkrun) Server() error {
	mg.SerialDeps(Version)

	_, err := buildDarkrun()
	if err != nil {
		return fmt.Errorf("serverBuild: %w", err)
	}
	binaryPath, err := filepath.Abs(filepath.Join("bin", "server", binaryName))
	if err != nil {
		return fmt.Errorf("serverPath: %w", err)
	}
	installPath, err := filepath.Abs(filepath.Join("bin", "game"))
	if err != nil {
		return fmt.Errorf("gamePath: %w", err)
	}

	arguments := []string{
		"server",
		"--game-path", installPath,
		"--trace", "server.jsonl",
	}
	command := exec.Command(binaryPath, arguments...)
	command.Dir = filepath.Dir(binaryPath)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err = command.Run()
	if err != nil {
		return fmt.Errorf("serverRun: %w", err)
	}
	return nil
}

// Run starts the existing game build with the primary local account.
func (Tutorial) Run() error {
	err := launch([]string{
		"--auto-play",
		"--skip-cinematic",
		"--account", primaryLocalAccount,
		"--client-trace", filepath.Join("..", "server", "darkspin", "logs", "traces", "client.jsonl"),
	})
	if err != nil {
		return fmt.Errorf("gameLaunch: %w", err)
	}
	return nil
}

// Run2 starts the existing game build with the secondary local account.
func (Tutorial) Run2() error {
	err := launch([]string{
		"--auto-play",
		"--skip-cinematic",
		"--account", secondaryLocalAccount,
		"--client-trace", filepath.Join("..", "server", "darkspin", "logs", "traces", "client.jsonl"),
	})
	if err != nil {
		return fmt.Errorf("gameLaunch: %w", err)
	}
	return nil
}

// Build builds the generic launcher and Fang.
func (Darkspin) Build() error {
	mg.SerialDeps(Version)

	err := os.MkdirAll(filepath.Join("bin", "game"), 0o755)
	if err != nil {
		return fmt.Errorf("gameMkdir: %w", err)
	}
	linkerFlags, err := launcherLinkerFlags(buildVersionLinkerFlags())
	if err != nil {
		return fmt.Errorf("launcherFlags: %w", err)
	}
	err = buildLauncher(linkerFlags)
	if err != nil {
		return fmt.Errorf("launcherBuild: %w", err)
	}
	err = buildFang(true)
	if err != nil {
		return fmt.Errorf("fangBuild: %w", err)
	}
	return nil
}

// Run starts the generic launcher with cinematics skipped.
func (Darkspin) Run() error {
	err := launch([]string{"--skip-cinematic"})
	if err != nil {
		return fmt.Errorf("skipLaunch: %w", err)
	}
	return nil
}

// RunNormal starts the client with its original cinematic flow.
func (Darkspin) RunNormal() error {
	err := launch(nil)
	if err != nil {
		return fmt.Errorf("normalLaunch: %w", err)
	}
	return nil
}

func launch(arguments []string) error {
	binaryPath, err := filepath.Abs(filepath.Join("bin", "game", darkSpinBinaryName))
	if err != nil {
		return fmt.Errorf("launcherPath: %w", err)
	}

	command := exec.Command(binaryPath, arguments...)
	command.Dir = filepath.Dir(binaryPath)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err = command.Run()
	if err != nil {
		return fmt.Errorf("launcherRun: %w", err)
	}
	return nil
}

// Debug rebuilds both sides and runs a paired traced client/server session.
func (Tutorial) Debug() error {
	err := Build()
	if err != nil {
		return fmt.Errorf("debugBuild: %w", err)
	}

	serverPath, err := filepath.Abs(filepath.Join("bin", "server", binaryName))
	if err != nil {
		return fmt.Errorf("debugServerPath: %w", err)
	}
	installPath, err := filepath.Abs(filepath.Join("bin", "game"))
	if err != nil {
		return fmt.Errorf("debugGamePath: %w", err)
	}
	serverArguments := []string{
		"server",
		"--game-path", installPath,
		"--trace", "server.jsonl",
	}
	serverCommand := exec.Command(serverPath, serverArguments...)
	serverCommand.Dir = filepath.Dir(serverPath)
	serverCommand.Stdin = nil
	serverCommand.Stdout = os.Stdout
	serverCommand.Stderr = os.Stderr
	err = serverCommand.Start()
	if err != nil {
		return fmt.Errorf("debugServerStart: %w", err)
	}

	err = waitForServer("http://127.0.0.1:42127/api", 15*time.Second)
	if err != nil {
		stopProcess(serverCommand)
		return fmt.Errorf("debugServerWait: %w", err)
	}

	launcherPath, err := filepath.Abs(filepath.Join("bin", "game", darkSpinBinaryName))
	if err != nil {
		stopProcess(serverCommand)
		return fmt.Errorf("debugLauncherPath: %w", err)
	}
	launcherArguments := []string{
		"-client-trace", filepath.Join("..", "server", "darkspin", "logs", "traces", "client.jsonl"),
		"-skip-cinematic",
	}
	launcherCommand := exec.Command(launcherPath, launcherArguments...)
	launcherCommand.Dir = filepath.Dir(launcherPath)
	launcherCommand.Stdin = os.Stdin
	launcherCommand.Stdout = os.Stdout
	launcherCommand.Stderr = os.Stderr
	err = launcherCommand.Run()
	stopProcess(serverCommand)
	if err != nil {
		return fmt.Errorf("debugRun: %w", err)
	}
	return nil
}

func waitForServer(address string, timeout time.Duration) error {
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response, err := client.Get(address)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode < http.StatusInternalServerError {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("server unavailable at %s after %s", address, timeout)
}

func stopProcess(command *exec.Cmd) {
	if command == nil || command.Process == nil {
		return
	}
	_ = command.Process.Kill()
	_, _ = command.Process.Wait()
}
