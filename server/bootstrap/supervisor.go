// Package bootstrap supervises a dedicated server and its stopped-state updates.
package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/instance"
)

// Options describes the dedicated layout and the arguments forwarded to server.
type Options struct {
	RootPath        string
	ConfigPath      string
	ManifestURL     string
	ServerArgs      []string
	Logger          *slog.Logger
	StartupTimeout  time.Duration
	ShutdownTimeout time.Duration
	RetentionDays   int
	// BootstrapLease may be acquired by the CLI before preparing configuration.
	// Run closes this lease on return, including startup failures.
	BootstrapLease *instance.Lease
}

type supervisor struct {
	option      Options
	storage     *Storage
	binaryPath  string
	runtimePath string
}

type childResult struct {
	isClean     bool
	isHealthy   bool
	isCancelled bool
}

// Run owns the bootstrap lease until cancellation. Database and executable
// replacement only occur after the server exits and its runtime lease is held.
func Run(ctx context.Context, req Options) error {
	if runtime.GOOS != "linux" {
		return errors.New("dedicated bootstrap updates require Linux; this platform supports direct server mode")
	}
	if strings.TrimSpace(req.RootPath) == "" {
		return errors.New("bootstrap root is required")
	}
	rootPath, err := filepath.Abs(req.RootPath)
	if err != nil {
		return fmt.Errorf("rootPath: %w", err)
	}
	req.RootPath = rootPath
	if req.ConfigPath == "" {
		req.ConfigPath = filepath.Join(rootPath, "runtime", "darkspin.toml")
	}
	req.ConfigPath, err = filepath.Abs(req.ConfigPath)
	if err != nil {
		return fmt.Errorf("configPath: %w", err)
	}
	if req.ConfigPath != filepath.Join(rootPath, "runtime", "darkspin.toml") {
		return errors.New("bootstrap configuration must be root/runtime/darkspin.toml so backups cover the complete runtime")
	}
	if req.Logger == nil {
		req.Logger = slog.Default()
	}
	if req.StartupTimeout <= 0 {
		req.StartupTimeout = 30 * time.Minute
	}
	if req.ShutdownTimeout <= 0 {
		req.ShutdownTimeout = 90 * time.Second
	}
	if req.RetentionDays <= 0 {
		req.RetentionDays = 7
	}
	storage, err := NewStorage(rootPath, req.RetentionDays)
	if err != nil {
		return fmt.Errorf("storageOpen: %w", err)
	}
	bootstrapLease := req.BootstrapLease
	if bootstrapLease == nil {
		bootstrapLease, err = instance.Acquire(filepath.Join(rootPath, "bootstrap-lock"))
		if err != nil {
			return fmt.Errorf("bootstrapLease: %w", err)
		}
	}
	defer closeLease(bootstrapLease, req.Logger)
	e := &supervisor{option: req, storage: storage,
		binaryPath:  filepath.Join(rootPath, "bin", installedBinaryName()),
		runtimePath: filepath.Join(filepath.Dir(req.ConfigPath), "darkspin")}
	err = e.recoverStopped()
	if err != nil {
		return fmt.Errorf("startupRecover: %w", err)
	}
	isFirstBoot, err := e.updateStopped(ctx)
	if err != nil {
		req.Logger.Error("startup update failed; restoring installed server", "error", err)
		err = e.rollbackStopped()
		if err != nil {
			return fmt.Errorf("startupRollback: %w", err)
		}
	}
	err = e.run(ctx, isFirstBoot)
	if err != nil {
		return fmt.Errorf("supervisorRun: %w", err)
	}
	return nil
}

func closeLease(lease *instance.Lease, logger *slog.Logger) {
	err := lease.Close()
	if err != nil {
		logger.Error("release runtime lease", "error", err)
	}
}

func (e *supervisor) recoverStopped() error {
	lease, err := instance.Acquire(e.runtimePath)
	if err != nil {
		return fmt.Errorf("recoveryLease: %w", err)
	}
	defer closeLease(lease, e.option.Logger)
	isRecovered, err := e.storage.Recover()
	if err != nil {
		return fmt.Errorf("candidateRecover: %w", err)
	}
	if isRecovered {
		e.option.Logger.Warn("restored matching executable and database after interrupted first boot")
		err = e.discardRequest()
		if err != nil {
			return fmt.Errorf("staleRequest: %w", err)
		}
	}
	return nil
}

func (e *supervisor) run(ctx context.Context, isFirstBoot bool) error {
	backoff := time.Second
	for ctx.Err() == nil {
		result := e.runChild(ctx, isFirstBoot)
		if result.isCancelled {
			// Keep firstboot persisted: the next supervisor conservatively restores it.
			return nil
		}
		if isFirstBoot && !result.isHealthy {
			err := e.rollbackStopped()
			if err != nil {
				return fmt.Errorf("firstbootRollback: %w", err)
			}
			isFirstBoot = false
			continue
		}
		isFirstBoot = false
		if result.isClean && result.isHealthy {
			var err error
			isFirstBoot, err = e.updateStopped(ctx)
			if err != nil {
				e.option.Logger.Error("maintenance update failed; restarting installed server", "error", err)
				// A failed install may already have written firstboot. Restore before restart.
				err = e.rollbackStopped()
				if err != nil {
					return fmt.Errorf("updateRollback: %w", err)
				}
			}
		} else {
			err := e.discardRequest()
			if err != nil {
				return fmt.Errorf("crashRequest: %w", err)
			}
		}
		if result.isHealthy {
			backoff = time.Second
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		if backoff < time.Minute {
			backoff *= 2
			if backoff > time.Minute {
				backoff = time.Minute
			}
		}
	}
	return nil
}

func (e *supervisor) rollbackStopped() error {
	lease, err := instance.Acquire(e.runtimePath)
	if err != nil {
		return fmt.Errorf("rollbackLease: %w", err)
	}
	defer closeLease(lease, e.option.Logger)
	isRecovered, err := e.storage.Recover()
	if err != nil {
		return fmt.Errorf("candidateRollback: %w", err)
	}
	if isRecovered {
		e.option.Logger.Warn("restored matching executable and database after failed candidate")
		err = e.discardRequest()
		if err != nil {
			return fmt.Errorf("rollbackRequest: %w", err)
		}
	}
	return nil
}

func (e *supervisor) updateStopped(ctx context.Context) (bool, error) {
	lease, err := instance.Acquire(e.runtimePath)
	if err != nil {
		return false, fmt.Errorf("updateLease: %w", err)
	}
	defer closeLease(lease, e.option.Logger)
	payload, err := os.ReadFile(filepath.Join(e.option.RootPath, "update-request.json"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("requestRead: %w", err)
	}
	var request struct {
		Version         int       `json:"version"`
		BuildID         string    `json:"build_id"`
		RequestedAt     time.Time `json:"requested_at"`
		IsCleanShutdown bool      `json:"is_clean_shutdown"`
	}
	err = json.Unmarshal(payload, &request)
	if err != nil {
		return false, fmt.Errorf("requestDecode: %w", err)
	}
	if request.Version != 1 || request.BuildID == "" || !request.IsCleanShutdown || request.RequestedAt.IsZero() {
		return false, errors.New("maintenance update request has no valid clean shutdown identity")
	}
	buildCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	buildCmd := exec.CommandContext(buildCtx, e.binaryPath, "build-info")
	buildCmd.Dir = filepath.Dir(e.option.ConfigPath)
	buildCmd.Env = e.childEnvs()
	identityPayload, err := buildCmd.Output()
	if err != nil {
		return false, fmt.Errorf("identityRead: %w", err)
	}
	var identity struct {
		BuildID string `json:"build_id"`
	}
	err = json.Unmarshal(identityPayload, &identity)
	if err != nil {
		return false, fmt.Errorf("identityDecode: %w", err)
	}
	if identity.BuildID != request.BuildID {
		return false, errors.New("maintenance request does not match the installed server build")
	}
	backupID, err := e.storage.Snapshot(request.BuildID)
	if err != nil {
		return false, fmt.Errorf("runtimeSnapshot: %w", err)
	}
	err = e.discardRequest()
	if err != nil {
		return false, fmt.Errorf("requestConsume: %w", err)
	}
	if e.option.ManifestURL == "" {
		e.pruneCompleted()
		return false, nil
	}
	isUpdated, err := e.storage.CheckUpdate(ctx, e.option.ManifestURL, backupID)
	if err != nil {
		return false, fmt.Errorf("candidateInstall: %w", err)
	}
	if !isUpdated {
		e.pruneCompleted()
	}
	return isUpdated, nil
}

func (e *supervisor) discardRequest() error {
	err := os.Remove(filepath.Join(e.option.RootPath, "update-request.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("requestRemove: %w", err)
	}
	err = syncDirectory(e.option.RootPath)
	if err != nil {
		return fmt.Errorf("requestSync: %w", err)
	}
	return nil
}

func (e *supervisor) runChild(ctx context.Context, isFirstBoot bool) childResult {
	result := childResult{}
	if ctx.Err() != nil {
		result.isCancelled = true
		return result
	}
	err := executableExists(e.binaryPath)
	if err != nil {
		e.option.Logger.Error("server executable unavailable", "error", err)
		return result
	}
	// A clean-stop request can survive a failed snapshot while stopped, but
	// cannot certify cleanliness once another child may mutate the databases.
	err = e.discardRequest()
	if err != nil {
		e.option.Logger.Error("refusing to start server with stale maintenance request", "error", err)
		return result
	}
	args := append([]string{"server"}, e.option.ServerArgs...)
	args = append(args, "--config", e.option.ConfigPath)
	cmd := exec.Command(e.binaryPath, args...)
	cmd.Dir = filepath.Dir(e.option.ConfigPath)
	cmd.Env = e.childEnvs()
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	configureChild(cmd)
	err = cmd.Start()
	if err != nil {
		e.option.Logger.Error("start server", "error", err)
		return result
	}
	exits := make(chan error, 1)
	go waitChild(cmd, exits)
	startup := time.NewTimer(e.option.StartupTimeout)
	defer startup.Stop()
	probe := time.NewTicker(5 * time.Second)
	defer probe.Stop()
	successCount, failureCount := 0, 0
	for {
		select {
		case <-ctx.Done():
			result.isClean = e.stopChild(cmd, exits)
			result.isCancelled = true
			return result
		case err = <-exits:
			result.isClean = err == nil
			e.option.Logger.Info("server exited", "pid", cmd.Process.Pid, "error", err)
			return result
		case <-startup.C:
			e.option.Logger.Error("server did not become ready before startup deadline")
			result.isClean = e.stopChild(cmd, exits)
			return result
		case <-probe.C:
			err = e.probe(ctx)
			if err != nil {
				successCount = 0
				failureCount++
				if result.isHealthy && failureCount >= 3 {
					e.option.Logger.Error("server lost readiness", "error", err)
					// Scheduled shutdown can lose readiness before listeners and
					// storage finish closing. Let it exit without cancelling the
					// clean-stop request through an external signal.
					return e.waitClosingChild(ctx, cmd, exits)
				}
				continue
			}
			failureCount = 0
			successCount++
			if result.isHealthy || successCount < 3 {
				continue
			}
			startup.Stop()
			e.option.Logger.Info("server readiness stable", "pid", cmd.Process.Pid)
			if isFirstBoot {
				err = e.storage.ConfirmHealthy()
				if err != nil {
					e.option.Logger.Error("complete first boot bookkeeping", "error", err)
					result.isClean = e.stopChild(cmd, exits)
					return result
				}
			}
			e.pruneCompleted()
			result.isHealthy = true
		}
	}
}

func (e *supervisor) waitClosingChild(ctx context.Context, cmd *exec.Cmd, exits <-chan error) childResult {
	result := childResult{isHealthy: true}
	timer := time.NewTimer(e.option.ShutdownTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		result.isClean = e.stopChild(cmd, exits)
		result.isCancelled = true
		return result
	case err := <-exits:
		result.isClean = err == nil
		e.option.Logger.Info("closing server exited", "error", err)
		return result
	case <-timer.C:
	}
	err := killChild(cmd)
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		e.option.Logger.Error("force closing server shutdown", "error", err)
	}
	err = <-exits
	if err != nil {
		e.option.Logger.Warn("closing server exceeded shutdown deadline", "error", err)
	}
	return result
}

func (e *supervisor) pruneCompleted() {
	err := e.storage.Prune()
	if err != nil {
		e.option.Logger.Warn("prune completed backups", "error", err)
	}
}

func waitChild(cmd *exec.Cmd, exits chan<- error) {
	exits <- cmd.Wait()
}

func (e *supervisor) probe(ctx context.Context) error {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, e.binaryPath,
		"maintenance", "status", "--ready", "--config", e.option.ConfigPath)
	cmd.Dir = filepath.Dir(e.option.ConfigPath)
	cmd.Env = e.childEnvs()
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("readinessProbe: %w", err)
	}
	return nil
}

func (e *supervisor) childEnvs() []string {
	envs := os.Environ()
	forwardedEnvs := make([]string, 0, len(envs)+1)
	for _, setting := range envs {
		if strings.HasPrefix(strings.ToUpper(setting), "DARKSPIN_BOOTSTRAP_ROOT=") {
			continue
		}
		forwardedEnvs = append(forwardedEnvs, setting)
	}
	return append(forwardedEnvs, "DARKSPIN_BOOTSTRAP_ROOT="+e.option.RootPath)
}

func (e *supervisor) stopChild(cmd *exec.Cmd, exits <-chan error) bool {
	err := signalChild(cmd)
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		e.option.Logger.Warn("signal server shutdown", "error", err)
	}
	timer := time.NewTimer(e.option.ShutdownTimeout)
	defer timer.Stop()
	select {
	case err = <-exits:
		if err != nil {
			e.option.Logger.Warn("server stopped with error", "error", err)
		}
		return err == nil
	case <-timer.C:
	}
	err = killChild(cmd)
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		e.option.Logger.Error("force server shutdown", "error", err)
	}
	err = <-exits
	if err != nil {
		e.option.Logger.Warn("server killed after shutdown deadline", "error", err)
	}
	return false
}
