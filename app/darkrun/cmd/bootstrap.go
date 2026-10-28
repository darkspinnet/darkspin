package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/bootstrap"
	postgresbackup "github.com/darkspinnet/darkspin/server/bootstrap/postgres"
	"github.com/darkspinnet/darkspin/server/buildinfo"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/instance"
	"github.com/darkspinnet/darkspin/server/maintenance"
	"github.com/darkspinnet/darkspin/server/runtime/playerstore"
	"github.com/spf13/cobra"
)

type bootstrapOptions struct {
	setup              serverSetupOptions
	rootPath           string
	configPath         string
	gamePath           string
	updateCron         string
	warningMinuteCount int
	startupSecondCount int
	retentionDayCount  int
	manifestURL        string
	isUpdateEnabled    bool
}

func newBootstrapCommand() *cobra.Command {
	e := &bootstrapOptions{}
	command := &cobra.Command{Use: "bootstrap", Short: "Supervise a dedicated server with PostgreSQL backups and updates", Args: cobra.NoArgs, RunE: e.run}
	e.setup.registerFlags(command)
	command.Flags().StringVar(&e.rootPath, "path", "./darkspin", "bootstrap root containing bin, runtime, and backup")
	command.Flags().StringVar(&e.configPath, "config", "", "must be PATH/runtime/darkspin.toml")
	command.Flags().StringVar(&e.gamePath, "game-path", "", "required readable build-103 installation")
	command.Flags().StringVar(&e.updateCron, "update-cron", "30 3 * * *", "UTC cron time to begin maintenance warnings")
	command.Flags().IntVar(&e.warningMinuteCount, "update-warning-minutes", 30, "warning interval before shutdown")
	command.Flags().IntVar(&e.startupSecondCount, "update-start-timeout-seconds", 1800, "candidate readiness deadline")
	command.Flags().IntVar(&e.retentionDayCount, "backup-retention-days", 7, "completed backup retention")
	command.Flags().StringVar(&e.manifestURL, "update-manifest-url", "", "HTTPS update manifest or manifest ZIP URL")
	command.Flags().BoolVar(&e.isUpdateEnabled, "update-enabled", true, "enable scheduled backups and updates")
	return command
}

func newBuildInfoCommand() *cobra.Command {
	return &cobra.Command{Use: "build-info", Short: "Print this executable's build identity as JSON", Args: cobra.NoArgs, RunE: runBuildInfo}
}

func runBuildInfo(command *cobra.Command, args []string) error {
	identity := struct {
		BuildID  string `json:"build_id"`
		Version  string `json:"version"`
		Platform string `json:"platform"`
	}{buildinfo.ID, buildinfo.Version, runtime.GOOS + "-" + runtime.GOARCH}
	err := json.NewEncoder(command.OutOrStdout()).Encode(identity)
	if err != nil {
		return fmt.Errorf("identityWrite: %w", err)
	}
	return nil
}

func (e *bootstrapOptions) run(command *cobra.Command, args []string) error {
	configuration, err := playerstore.FromEnvironment()
	if err != nil {
		return fmt.Errorf("postgresEnvironment: %w", err)
	}
	if runtime.GOOS != "linux" {
		return errors.New("dedicated bootstrap requires Linux; use server mode on this platform")
	}
	databaseBackup, err := postgresbackup.New(command.Context(), configuration)
	if err != nil {
		return fmt.Errorf("postgresBackup: %w", err)
	}
	err = applyBootstrapEnvironment(command)
	if err != nil {
		return fmt.Errorf("bootstrapEnvironment: %w", err)
	}
	if e.setup.isPrepareOnly {
		return errors.New("bootstrap does not support --prepare-only; use server --prepare-only")
	}
	rootPath, err := filepath.Abs(e.rootPath)
	if err != nil {
		return fmt.Errorf("bootstrapPath: %w", err)
	}
	configPath := filepath.Join(rootPath, "runtime", game.DefaultConfigFilename)
	if strings.TrimSpace(e.configPath) != "" {
		selectedPath, pathErr := filepath.Abs(e.configPath)
		if pathErr != nil {
			return fmt.Errorf("configAbs: %w", pathErr)
		}
		if selectedPath != configPath {
			return errors.New("bootstrap config must be PATH/runtime/darkspin.toml")
		}
	}
	storage, err := bootstrap.NewStorage(rootPath, 7)
	if err != nil {
		return fmt.Errorf("bootstrapLayout: %w", err)
	}
	storage.DatabaseBackup = databaseBackup
	lease, err := instance.Acquire(filepath.Join(rootPath, "bootstrap-lock"))
	if err != nil {
		return fmt.Errorf("bootstrapLease: %w", err)
	}
	defer closeBootstrapLease(lease, command)
	options, err := e.prepare(command, rootPath, configPath, storage)
	if err != nil {
		return fmt.Errorf("bootstrapPrepare: %w", err)
	}
	options.BootstrapLease = lease
	options.DatabaseBackup = databaseBackup
	options.Logger = slog.New(slog.NewTextHandler(command.ErrOrStderr(), nil))
	err = bootstrap.Run(command.Context(), options)
	if err != nil {
		return fmt.Errorf("bootstrapRun: %w", err)
	}
	return nil
}

func closeBootstrapLease(lease *instance.Lease, command *cobra.Command) {
	err := lease.Close()
	if err != nil {
		slog.New(slog.NewTextHandler(command.ErrOrStderr(), nil)).Error("release bootstrap lease", "error", err)
	}
}

func (e *bootstrapOptions) prepare(command *cobra.Command, rootPath, configPath string, storage *bootstrap.Storage) (bootstrap.Options, error) {
	options := bootstrap.Options{RootPath: rootPath, ConfigPath: configPath}
	lease, err := instance.Acquire(filepath.Join(rootPath, "runtime", "darkspin"))
	if err != nil {
		return options, fmt.Errorf("settingsLease: %w", err)
	}
	defer closeBootstrapLease(lease, command)
	isRecovered, err := storage.RecoverContext(command.Context())
	if err != nil {
		return options, fmt.Errorf("settingsRecover: %w", err)
	}
	if isRecovered {
		err = os.Remove(filepath.Join(rootPath, "update-request.json"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return options, fmt.Errorf("recoveredRequest: %w", err)
		}
		if err == nil {
			r, openErr := os.Open(rootPath)
			if openErr != nil {
				return options, fmt.Errorf("requestDirectory: %w", openErr)
			}
			syncErr := r.Sync()
			closeErr := r.Close()
			if syncErr != nil || closeErr != nil {
				return options, fmt.Errorf("requestDurability: %w", errors.Join(syncErr, closeErr))
			}
		}
	}
	err = validateBootstrapPaths(command, rootPath, configPath, e.setup.contentPath)
	if err != nil {
		return options, fmt.Errorf("settingsPath: %w", err)
	}
	config, err := e.setup.prepare(command, configPath)
	if err != nil {
		return options, fmt.Errorf("serverSettings: %w", err)
	}
	if !config.Bool(game.ConfigServerIsDedicated) {
		return options, errors.New("PostgreSQL bootstrap requires --dedicated")
	}
	settings := []struct {
		flag    string
		key     game.ConfigKey
		setting string
	}{
		{"update-enabled", game.ConfigBootstrapIsUpdateEnabled, strconv.FormatBool(e.isUpdateEnabled)},
		{"update-cron", game.ConfigBootstrapUpdateCron, e.updateCron},
		{"update-warning-minutes", game.ConfigBootstrapWarningMinutes, strconv.Itoa(e.warningMinuteCount)},
		{"update-start-timeout-seconds", game.ConfigBootstrapStartupTimeoutSeconds, strconv.Itoa(e.startupSecondCount)},
		{"backup-retention-days", game.ConfigBootstrapBackupRetentionDays, strconv.Itoa(e.retentionDayCount)},
		{"update-manifest-url", game.ConfigBootstrapManifestURL, e.manifestURL},
	}
	for _, setting := range settings {
		if !command.Flags().Changed(setting.flag) {
			continue
		}
		err = config.Set(setting.key, strings.TrimSpace(setting.setting))
		if err != nil {
			return options, fmt.Errorf("bootstrapSet[%s]: %w", setting.flag, err)
		}
	}
	schedule, err := bootstrapSchedule(config, rootPath)
	if err != nil {
		return options, fmt.Errorf("scheduleSettings: %w", err)
	}
	if schedule != nil {
		options.ManifestURL = config.String(game.ConfigBootstrapManifestURL)
	}
	startupSecondCount, err := config.Uint32(game.ConfigBootstrapStartupTimeoutSeconds)
	if err != nil {
		return options, fmt.Errorf("startupSeconds: %w", err)
	}
	if startupSecondCount < 1 || startupSecondCount > 86400 {
		return options, errors.New("bootstrap startup timeout must be between 1 and 86400 seconds")
	}
	retentionDayCount, err := config.Uint32(game.ConfigBootstrapBackupRetentionDays)
	if err != nil {
		return options, fmt.Errorf("retentionDays: %w", err)
	}
	if retentionDayCount < 1 || retentionDayCount > 3650 {
		return options, errors.New("backup retention must be between 1 and 3650 days")
	}
	options.StartupTimeout = time.Duration(startupSecondCount) * time.Second
	options.RetentionDays = int(retentionDayCount)
	gamePath, err := resolveContentGamePath(e.gamePath)
	if err != nil {
		return options, fmt.Errorf("gamePath: %w", err)
	}
	options.ServerArgs = []string{"--game-path", gamePath}
	err = config.Save(configPath)
	if err != nil {
		return options, fmt.Errorf("bootstrapSave: %w", err)
	}
	return options, nil
}

func applyBootstrapEnvironment(command *cobra.Command) error {
	err := applyServerEnvironment(command)
	if err != nil {
		return fmt.Errorf("serverEnvironment: %w", err)
	}
	settings := []struct {
		flag     string
		variable string
	}{
		{"path", "DARKSPIN_BOOTSTRAP_PATH"}, {"update-cron", "DARKSPIN_UPDATE_CRON"},
		{"update-warning-minutes", "DARKSPIN_UPDATE_WARNING_MINUTES"},
		{"update-start-timeout-seconds", "DARKSPIN_UPDATE_START_TIMEOUT_SECONDS"},
		{"backup-retention-days", "DARKSPIN_BACKUP_RETENTION_DAYS"},
		{"update-manifest-url", "DARKSPIN_UPDATE_MANIFEST_URL"}, {"update-enabled", "DARKSPIN_IS_UPDATE_ENABLED"},
	}
	for _, setting := range settings {
		if command.Flags().Changed(setting.flag) {
			continue
		}
		argument := strings.TrimSpace(os.Getenv(setting.variable))
		if argument == "" {
			continue
		}
		err = command.Flags().Set(setting.flag, argument)
		if err != nil {
			return fmt.Errorf("bootstrapEnvironment[%s]: %w", setting.variable, err)
		}
	}
	return nil
}

func bootstrapSchedule(config *game.Config, rootPath string) (*maintenance.ScheduleOptions, error) {
	warningMinuteCount, err := config.Uint32(game.ConfigBootstrapWarningMinutes)
	if err != nil {
		return nil, fmt.Errorf("warningMinutes: %w", err)
	}
	req := maintenance.ScheduleOptions{Cron: config.String(game.ConfigBootstrapUpdateCron), WarningMinutes: int(warningMinuteCount), UpdateRequestPath: filepath.Join(rootPath, "update-request.json")}
	err = maintenance.ValidateSchedule(req)
	if err != nil {
		return nil, fmt.Errorf("scheduleValidate: %w", err)
	}
	manifestURL, err := url.Parse(config.String(game.ConfigBootstrapManifestURL))
	if err != nil {
		return nil, fmt.Errorf("manifestParse: %w", err)
	}
	if manifestURL.Scheme != "https" || manifestURL.Host == "" || manifestURL.User != nil || manifestURL.Fragment != "" {
		return nil, errors.New("bootstrap manifest URL must use HTTPS without credentials or fragment")
	}
	if !config.Bool(game.ConfigBootstrapIsUpdateEnabled) {
		// Disable automatic occurrences, while keeping bootstrap's explicit
		// update command and configured manifest available to operators.
		req.Cron = ""
	}
	return &req, nil
}

func validateBootstrapPaths(command *cobra.Command, rootPath, configPath, requestedContentPath string) error {
	if configPath != filepath.Join(rootPath, "runtime", game.DefaultConfigFilename) {
		return errors.New("bootstrap requires PATH/runtime/darkspin.toml")
	}
	config := game.DefaultConfig()
	fi, err := os.Stat(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("layoutStat: %w", err)
	}
	if err == nil {
		if !fi.Mode().IsRegular() {
			return errors.New("bootstrap config must be a regular file")
		}
		var isCreated bool
		config, isCreated, err = game.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("layoutLoad: %w", err)
		}
		if isCreated {
			return errors.New("bootstrap config disappeared while validating")
		}
	}
	if command.Flags().Changed("content-path") {
		requestedContentPath = strings.TrimSpace(requestedContentPath)
		if requestedContentPath != "" {
			requestedContentPath, err = filepath.Abs(requestedContentPath)
			if err != nil {
				return fmt.Errorf("requestedContent: %w", err)
			}
		}
		err = config.Set(game.ConfigContentPath, requestedContentPath)
		if err != nil {
			return fmt.Errorf("layoutContent: %w", err)
		}
	}
	contentPath, err := config.ContentPath(configPath)
	if err != nil {
		return fmt.Errorf("layoutResolve: %w", err)
	}
	if contentPath != filepath.Join(rootPath, "runtime", "darkspin", "cache") {
		return errors.New("bootstrap content must remain in PATH/runtime/darkspin/cache")
	}
	if config.String(game.ConfigStorageDriver) != "sqlite" {
		return errors.New("bootstrap requires the local SQLite content driver; player storage uses PostgreSQL")
	}
	return nil
}
