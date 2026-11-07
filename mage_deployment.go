//go:build mage

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/magefile/mage/mg"
)

// Deployment manages the isolated local Compose deployment in bin/deployment.
type Deployment mg.Namespace

type deploymentCommand struct {
	root       string
	dockerPath string
}

// Start builds the current Linux server and starts PostgreSQL and bootstrap.
// Stop the deployment before invoking Start to rebuild a running server.
func (Deployment) Start() error {
	command, err := newDeploymentCommand()
	if err != nil {
		return fmt.Errorf("deploymentInit: %w", err)
	}
	err = command.prepare()
	if err != nil {
		return fmt.Errorf("deploymentPrepare: %w", err)
	}
	check := command.composeCommand("ps", "--status", "running", "-q")
	output, err := check.CombinedOutput()
	if err != nil {
		return fmt.Errorf("deploymentInspect: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if strings.TrimSpace(string(output)) != "" {
		return errors.New("deployment is already running; use mage deployment:stop before rebuilding")
	}
	err = command.build()
	if err != nil {
		return fmt.Errorf("deploymentBuild: %w", err)
	}
	err = command.run("up", "-d", "--build", "--wait", "--wait-timeout", "1800")
	if err != nil {
		return fmt.Errorf("deploymentStart: %w", err)
	}
	return nil
}

// Stop stops only this deployment's containers and preserves all bind data.
func (Deployment) Stop() error {
	command, err := newDeploymentCommand()
	if err != nil {
		return fmt.Errorf("deploymentInit: %w", err)
	}
	err = command.run("stop")
	if err != nil {
		return fmt.Errorf("deploymentStop: %w", err)
	}
	return nil
}

// Status shows this deployment's containers, including failed/stopped services.
func (Deployment) Status() error {
	command, err := newDeploymentCommand()
	if err != nil {
		return fmt.Errorf("deploymentInit: %w", err)
	}
	err = command.run("ps", "-a")
	if err != nil {
		return fmt.Errorf("deploymentStatus: %w", err)
	}
	return nil
}

// Logs prints the latest daily server log without requiring Docker.
func (Deployment) Logs() error {
	repositoryRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("repositoryPath: %w", err)
	}
	logPath := filepath.Join(repositoryRoot, "bin", "deployment", "runtime", "runtime", "darkspin", "logs", "logs")
	entries, err := os.ReadDir(logPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("logDirectory: %w", err)
	}
	latestName := ""
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasSuffix(name, ".txt") {
			continue
		}
		date, parseErr := time.Parse(time.DateOnly, strings.TrimSuffix(name, ".txt"))
		if parseErr != nil || date.Format(time.DateOnly)+".txt" != name {
			continue
		}
		if name > latestName {
			latestName = name
		}
	}
	if latestName == "" {
		noticeCount, noticeErr := fmt.Fprintln(os.Stdout, "No daily server log is available yet; logs appear after the updated dedicated server starts.")
		if noticeErr != nil {
			return fmt.Errorf("logNotice[%d]: %w", noticeCount, noticeErr)
		}
		return nil
	}
	err = printDeploymentLogTail(filepath.Join(logPath, latestName))
	if err != nil {
		return fmt.Errorf("deploymentLogs: %w", err)
	}
	return nil
}

func printDeploymentLogTail(path string) (resultErr error) {
	r, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("logOpen: %w", err)
	}
	defer func() {
		closeErr := r.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("logClose: %w", closeErr))
		}
	}()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("logStat: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return errors.New("daily log is not a regular file")
	}
	const byteLimit int64 = 64 * 1024
	offset := max(fi.Size()-byteLimit, 0)
	readOffset, err := r.Seek(offset, io.SeekStart)
	if err != nil {
		return fmt.Errorf("logSeek: %w", err)
	}
	if readOffset != offset {
		return errors.New("daily log seek did not reach the requested offset")
	}
	payload, err := io.ReadAll(io.LimitReader(r, byteLimit))
	if err != nil {
		return fmt.Errorf("logRead: %w", err)
	}
	if offset > 0 {
		lineEnd := bytes.IndexByte(payload, '\n')
		if lineEnd >= 0 && lineEnd < len(payload)-1 {
			payload = payload[lineEnd+1:]
		}
		limitCount, limitErr := fmt.Fprintln(os.Stdout, "[Showing at most the last 64 KiB and 100 lines of the daily log.]")
		if limitErr != nil {
			return fmt.Errorf("logLimit[%d]: %w", limitCount, limitErr)
		}
	}
	lines := bytes.SplitAfter(payload, []byte{'\n'})
	if len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > 100 {
		lines = lines[len(lines)-100:]
	}
	tail := bytes.Join(lines, nil)
	writtenCount, err := os.Stdout.Write(tail)
	if err != nil {
		return fmt.Errorf("logPrint: %w", err)
	}
	if writtenCount != len(tail) {
		return fmt.Errorf("logShort: %w", io.ErrShortWrite)
	}
	return nil
}

func newDeploymentCommand() (*deploymentCommand, error) {
	repositoryRoot, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("repositoryPath: %w", err)
	}
	dockerPath, err := exec.LookPath("docker")
	if err != nil && runtime.GOOS == "windows" {
		homePath, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return nil, fmt.Errorf("dockerHome: %w", homeErr)
		}
		candidates := []string{
			filepath.Join(homePath, "AppData", "Local", "Programs", "DockerDesktop", "resources", "bin", "docker.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Docker", "Docker", "resources", "bin", "docker.exe"),
		}
		for _, candidate := range candidates {
			fi, statErr := os.Stat(candidate)
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			if statErr != nil {
				return nil, fmt.Errorf("dockerStat: %w", statErr)
			}
			if fi.IsDir() {
				continue
			}
			dockerPath = candidate
			err = nil
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("dockerPath: %w", err)
	}
	return &deploymentCommand{root: filepath.Join(repositoryRoot, "bin", "deployment"), dockerPath: dockerPath}, nil
}

func (e *deploymentCommand) arguments(arguments ...string) []string {
	return append([]string{"compose", "--project-name", "darkspin-deployment", "--project-directory", e.root,
		"--env-file", filepath.Join(e.root, ".env"), "-f", filepath.Join(e.root, "compose.yaml"),
		"-f", filepath.Join(e.root, "compose.local.yaml")}, arguments...)
}

func (e *deploymentCommand) composeCommand(arguments ...string) *exec.Cmd {
	command := exec.Command(e.dockerPath, e.arguments(arguments...)...)
	// Docker Desktop's credential helper lives beside its CLI. The current shell
	// may predate installation and lack that directory in PATH.
	command.Env = append(os.Environ(), "PATH="+filepath.Dir(e.dockerPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return command
}

func (e *deploymentCommand) prepare() error {
	for _, directory := range []string{"runtime/bin", "postgres", "logs", "image", "build-cache"} {
		err := os.MkdirAll(filepath.Join(e.root, directory), 0o755)
		if err != nil {
			return fmt.Errorf("deploymentDirectory: %w", err)
		}
	}
	repositoryRoot := filepath.Dir(filepath.Dir(e.root))
	for _, filename := range []string{"compose.yaml", "Dockerfile", "entrypoint.sh"} {
		contents, err := os.ReadFile(filepath.Join(repositoryRoot, "deployment", filename))
		if err != nil {
			return fmt.Errorf("sourceRead[%s]: %w", filename, err)
		}
		targetPath := filepath.Join(e.root, "image", filename)
		if filename == "compose.yaml" {
			targetPath = filepath.Join(e.root, filename)
		}
		contents = []byte(strings.ReplaceAll(string(contents), "\r\n", "\n"))
		err = os.WriteFile(targetPath, contents, 0o644)
		if err != nil {
			return fmt.Errorf("sourceCopy[%s]: %w", filename, err)
		}
	}
	err := e.prepareGame(filepath.Join(repositoryRoot, "bin", "game"))
	if err != nil {
		return fmt.Errorf("gameMount: %w", err)
	}
	err = e.prepareEnvironment()
	if err != nil {
		return fmt.Errorf("deploymentEnvironment: %w", err)
	}
	override := map[string]any{"services": map[string]any{
		"darkrun": map[string]any{
			"image":       "darkspin-deployment-bootstrap:local",
			"build":       map[string]any{"context": "./image", "dockerfile": "Dockerfile"},
			"environment": map[string]string{"DARKSPIN_IS_UPDATE_ENABLED": "false"},
		},
		"postgres": map[string]any{"volumes": []map[string]any{{
			"type": "bind", "source": "./postgres", "target": "/var/lib/postgresql",
			"bind": map[string]bool{"create_host_path": false},
		}}},
	}}
	contents, err := json.MarshalIndent(override, "", "  ")
	if err != nil {
		return fmt.Errorf("overrideEncode: %w", err)
	}
	err = os.WriteFile(filepath.Join(e.root, "compose.local.yaml"), contents, 0o644)
	if err != nil {
		return fmt.Errorf("overrideWrite: %w", err)
	}
	return nil
}

func (e *deploymentCommand) prepareGame(gamePath string) error {
	for _, resource := range []string{"Data", "DarksporeBin/Darkspore.exe"} {
		fi, err := os.Stat(filepath.Join(gamePath, resource))
		if err != nil {
			return fmt.Errorf("gameResource[%s]: %w", resource, err)
		}
		if resource == "Data" && !fi.IsDir() {
			return errors.New("game Data path is not a directory")
		}
	}
	mountPath := filepath.Join(e.root, "game")
	fi, err := os.Lstat(mountPath)
	if err == nil {
		if fi.Mode()&os.ModeSymlink != 0 || fi.Mode()&os.ModeIrregular != 0 {
			targetFI, targetErr := os.Stat(mountPath)
			if targetErr != nil {
				return fmt.Errorf("mountTarget: %w", targetErr)
			}
			sourceFI, sourceErr := os.Stat(gamePath)
			if sourceErr != nil {
				return fmt.Errorf("mountSource: %w", sourceErr)
			}
			if !os.SameFile(sourceFI, targetFI) {
				return errors.New("deployment game link points to a different installation")
			}
			return nil
		}
		if !fi.IsDir() {
			return errors.New("deployment game mount is not a directory link")
		}
		entries, readErr := os.ReadDir(mountPath)
		if readErr != nil {
			return fmt.Errorf("mountRead: %w", readErr)
		}
		if len(entries) != 0 {
			return errors.New("deployment game directory already contains files; refusing to replace it")
		}
		err = os.Remove(mountPath) // Only the verified empty deployment directory.
		if err != nil {
			return fmt.Errorf("mountEmptyRemove: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("mountStat: %w", err)
	}
	if runtime.GOOS != "windows" {
		err = os.Symlink(gamePath, mountPath)
		if err != nil {
			return fmt.Errorf("mountSymlink: %w", err)
		}
		return nil
	}
	// Junction creation requires no developer-mode symlink privilege. Both paths
	// are quoted as PowerShell literals, including any embedded apostrophes.
	statement := "New-Item -ItemType Junction -Path '" + strings.ReplaceAll(mountPath, "'", "''") +
		"' -Target '" + strings.ReplaceAll(gamePath, "'", "''") + "' -ErrorAction Stop | Out-Null"
	err = runCommand("", "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", statement)
	if err != nil {
		return fmt.Errorf("mountJunction: %w", err)
	}
	return nil
}

func (e *deploymentCommand) prepareEnvironment() error {
	path := filepath.Join(e.root, ".env")
	fi, err := os.Stat(path)
	if err == nil {
		if !fi.Mode().IsRegular() {
			return errors.New("deployment environment is not a regular file")
		}
		return nil // Keep the database password and operator's explicit settings.
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("environmentStat: %w", err)
	}
	adapters, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("adapterList: %w", err)
	}
	var host string
	for _, adapter := range adapters {
		name := strings.ToLower(adapter.Name)
		if adapter.Flags&net.FlagUp == 0 || adapter.Flags&net.FlagLoopback != 0 || strings.Contains(name, "vethernet") || strings.Contains(name, "docker") || strings.Contains(name, "wsl") {
			continue
		}
		addresses, addressErr := adapter.Addrs()
		if addressErr != nil {
			return fmt.Errorf("adapterAddress: %w", addressErr)
		}
		for _, address := range addresses {
			ip, network, parseErr := net.ParseCIDR(address.String())
			if parseErr != nil {
				return fmt.Errorf("adapterParse: %w", parseErr)
			}
			if network == nil || ip.To4() == nil || !ip.IsGlobalUnicast() {
				continue
			}
			host = ip.String()
			break
		}
		if host != "" {
			break
		}
	}
	if host == "" {
		return errors.New("no advertised IPv4 found; create bin/deployment/.env with the deployment settings")
	}
	secret := make([]byte, 32)
	byteCount, err := rand.Read(secret)
	if err != nil {
		return fmt.Errorf("passwordGenerate: %w", err)
	}
	if byteCount != len(secret) {
		return errors.New("incomplete deployment password")
	}
	contents := "COMPOSE_PROJECT_NAME=darkspin-deployment\nDARKSPIN_ADVERTISED_HOST=" + host +
		"\nDARKSPIN_PORT=42137\nDARKSPIN_HOME_PATH=./runtime\nDARKSPIN_GAME_HOST_PATH=./game\n" +
		"POSTGRES_DB=darkspin\nPOSTGRES_USER=darkspin\nPOSTGRES_PASSWORD=" + hex.EncodeToString(secret) + "\n"
	err = os.WriteFile(path, []byte(contents), 0o600)
	if err != nil {
		return fmt.Errorf("environmentWrite: %w", err)
	}
	return nil
}

func (e *deploymentCommand) build() error {
	mg.SerialDeps((Darkrun{}).PrepareLua)
	err := Version()
	if err != nil {
		return fmt.Errorf("versionValidate: %w", err)
	}
	command := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w "+buildVersionLinkerFlags(),
		"-o", filepath.Join(e.root, "runtime", "bin", "darkrun"), "./app/darkrun")
	command.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0", "GOCACHE="+filepath.Join(e.root, "build-cache"))
	err = e.runLogged(command, "build.log")
	if err != nil {
		return fmt.Errorf("linuxBuild: %w", err)
	}
	return nil
}

func (e *deploymentCommand) run(arguments ...string) error {
	command := e.composeCommand(arguments...)
	err := e.runLogged(command, "compose.log")
	if err != nil {
		return fmt.Errorf("composeRun: %w", err)
	}
	return nil
}

func (e *deploymentCommand) runLogged(command *exec.Cmd, filename string) (resultErr error) {
	w, err := os.OpenFile(filepath.Join(e.root, "logs", filename), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("logOpen: %w", err)
	}
	defer func() {
		closeErr := w.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("logClose: %w", closeErr))
		}
	}()
	command.Stdout = io.MultiWriter(os.Stdout, w)
	command.Stderr = io.MultiWriter(os.Stderr, w)
	err = command.Run()
	if err != nil {
		return fmt.Errorf("processRun: %w", err)
	}
	return nil
}
