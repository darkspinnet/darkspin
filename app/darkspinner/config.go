package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/maintenance"
	server "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/darkspinnet/darkspin/server/snapshot"
)

// ServerConfiguration is the launcher-editable server and client configuration.
type ServerConfiguration struct {
	BindHost                       string         `json:"bindHost"`
	AdvertisedHost                 string         `json:"advertisedHost"`
	IsDedicated                    bool           `json:"isDedicated"`
	ContentPath                    string         `json:"contentPath"`
	JWTSecret                      string         `json:"jwtSecret"`
	Port                           uint16         `json:"port"`
	IsMultiplayerEnabled           bool           `json:"isMultiplayerEnabled"`
	Locale                         string         `json:"locale"`
	Locales                        []ClientLocale `json:"locales"`
	SnapshotMode                   string         `json:"snapshotMode"`
	IsBootstrapUpdateEnabled       bool           `json:"isBootstrapUpdateEnabled"`
	BootstrapUpdateCron            string         `json:"bootstrapUpdateCron"`
	BootstrapWarningMinutes        int            `json:"bootstrapWarningMinutes"`
	BootstrapStartupTimeoutSeconds int            `json:"bootstrapStartupTimeoutSeconds"`
	BootstrapBackupRetentionDays   int            `json:"bootstrapBackupRetentionDays"`
	BootstrapManifestURL           string         `json:"bootstrapManifestUrl"`
}

// GetServerConfiguration returns the persisted local network configuration.
func (e *App) GetServerConfiguration() (ServerConfiguration, error) {
	pathSet, err := e.spinnerPaths()
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("configPath: %w", err)
	}
	config, _, err := game.LoadConfig(pathSet.configPath)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("configLoad: %w", err)
	}
	configuration, err := serverConfiguration(config, pathSet.gamePath)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("configRead: %w", err)
	}
	return configuration, nil
}

// OpenFirewallSettings opens Windows' application firewall management page.
func (e *App) OpenFirewallSettings() error {
	err := openFirewallSettings()
	if err != nil {
		return fmt.Errorf("firewallOpen: %w", err)
	}
	return nil
}

// SetServerPort persists a new port and restarts only the embedded server.
func (e *App) SetServerPort(port uint16) (ServerConfiguration, error) {
	configuration, err := e.GetServerConfiguration()
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("configRead: %w", err)
	}
	return e.SetServerConfiguration(
		port, configuration.IsMultiplayerEnabled,
		configuration.Locale, configuration.SnapshotMode,
	)
}

// SetServerConfiguration persists launcher settings and restarts the embedded
// server only when its network binding changed.
func (e *App) SetServerConfiguration(
	port uint16, isMultiplayerEnabled bool, locale string, snapshotMode string,
) (ServerConfiguration, error) {
	req, err := e.GetServerConfiguration()
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("configRead: %w", err)
	}
	req.Port = port
	req.IsMultiplayerEnabled = isMultiplayerEnabled
	req.Locale = locale
	req.SnapshotMode = snapshotMode
	configuration, err := e.SetServerSetup(req)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("setupApply: %w", err)
	}
	return configuration, nil
}

// SetServerSetup edits the persisted setup through the same restart and
// rollback flow as the launcher network controls.
func (e *App) SetServerSetup(req ServerConfiguration) (ServerConfiguration, error) {
	port := req.Port
	isMultiplayerEnabled := req.IsMultiplayerEnabled
	locale := req.Locale
	snapshotMode := req.SnapshotMode
	if port == 0 || port == ^uint16(0) {
		return ServerConfiguration{}, errors.New("server port must be between 1 and 65534")
	}
	e.serverMu.Lock()
	defer e.serverMu.Unlock()

	e.mu.Lock()
	if e.isShuttingDown {
		e.mu.Unlock()
		return ServerConfiguration{}, errors.New("Darkspinner is shutting down")
	}
	if e.cancel != nil || e.gameDone != nil {
		e.mu.Unlock()
		return ServerConfiguration{}, errors.New("close active launcher and game operations before changing server configuration")
	}
	gameServer := e.serviceSet.gameServer
	e.mu.Unlock()
	if gameServer == nil {
		return ServerConfiguration{}, errors.New("local server is not initialized")
	}

	pathSet, err := e.spinnerPaths()
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("configPath: %w", err)
	}
	config, _, err := game.LoadConfig(pathSet.configPath)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("configLoad: %w", err)
	}
	err = validateBootstrapConfiguration(req, pathSet.configPath)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("bootstrapValidate: %w", err)
	}
	previousSettings := map[game.ConfigKey]string{
		game.ConfigServerBindHost:                 config.String(game.ConfigServerBindHost),
		game.ConfigServerAdvertisedHost:           config.String(game.ConfigServerAdvertisedHost),
		game.ConfigServerIsDedicated:              config.String(game.ConfigServerIsDedicated),
		game.ConfigContentPath:                    config.String(game.ConfigContentPath),
		game.ConfigAuthJWTSecret:                  config.String(game.ConfigAuthJWTSecret),
		game.ConfigBootstrapIsUpdateEnabled:       config.String(game.ConfigBootstrapIsUpdateEnabled),
		game.ConfigBootstrapUpdateCron:            config.String(game.ConfigBootstrapUpdateCron),
		game.ConfigBootstrapWarningMinutes:        config.String(game.ConfigBootstrapWarningMinutes),
		game.ConfigBootstrapStartupTimeoutSeconds: config.String(game.ConfigBootstrapStartupTimeoutSeconds),
		game.ConfigBootstrapBackupRetentionDays:   config.String(game.ConfigBootstrapBackupRetentionDays),
		game.ConfigBootstrapManifestURL:           config.String(game.ConfigBootstrapManifestURL),
	}
	settings := map[game.ConfigKey]string{
		game.ConfigServerBindHost:                 strings.TrimSpace(req.BindHost),
		game.ConfigServerAdvertisedHost:           strings.TrimSpace(req.AdvertisedHost),
		game.ConfigServerIsDedicated:              strconv.FormatBool(req.IsDedicated),
		game.ConfigContentPath:                    strings.TrimSpace(req.ContentPath),
		game.ConfigAuthJWTSecret:                  req.JWTSecret,
		game.ConfigBootstrapIsUpdateEnabled:       strconv.FormatBool(req.IsBootstrapUpdateEnabled),
		game.ConfigBootstrapUpdateCron:            strings.TrimSpace(req.BootstrapUpdateCron),
		game.ConfigBootstrapWarningMinutes:        strconv.Itoa(req.BootstrapWarningMinutes),
		game.ConfigBootstrapStartupTimeoutSeconds: strconv.Itoa(req.BootstrapStartupTimeoutSeconds),
		game.ConfigBootstrapBackupRetentionDays:   strconv.Itoa(req.BootstrapBackupRetentionDays),
		game.ConfigBootstrapManifestURL:           strings.TrimSpace(req.BootstrapManifestURL),
	}
	isSetupChanged := false
	isBootstrapChanged := false
	for key, setting := range settings {
		if strings.HasPrefix(string(key), "BOOTSTRAP_") {
			isBootstrapChanged = isBootstrapChanged || setting != previousSettings[key]
		} else {
			isSetupChanged = isSetupChanged || setting != previousSettings[key]
		}
		err = config.Set(key, setting)
		if err != nil {
			return ServerConfiguration{}, fmt.Errorf("setupSet: %w", err)
		}
	}
	bindHost, advertisedHost, err := config.NetworkHosts()
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("networkValidate: %w", err)
	}
	if bindHost == "" || advertisedHost == "" {
		return ServerConfiguration{}, errors.New("network hosts are empty")
	}
	isAuthPrepared, err := config.PrepareDedicatedAuth()
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("authPrepare: %w", err)
	}
	isSetupChanged = isSetupChanged || isAuthPrepared
	previousPort, err := config.Uint16(game.ConfigServerPort)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("previousPort: %w", err)
	}
	isPreviouslyMultiplayerEnabled := config.Bool(game.ConfigIsMultiplayerEnabled)
	previousLocale := config.String(game.ConfigClientLocale)
	previousSnapshotMode, err := snapshot.ParseMode(
		config.String(game.ConfigSnapshotMode),
	)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("previousSnapshotMode: %w", err)
	}
	selectedSnapshotMode, err := snapshot.ParseMode(snapshotMode)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("snapshotMode: %w", err)
	}
	locales, err := detectClientLocales(pathSet.gamePath)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("localeDetect: %w", err)
	}
	locale = normalizeClientLocale(locale)
	if !hasClientLocale(locales, locale) {
		return ServerConfiguration{}, fmt.Errorf("locale %q is not installed", locale)
	}
	if !isSetupChanged && !isBootstrapChanged && previousPort == port && isPreviouslyMultiplayerEnabled == isMultiplayerEnabled &&
		normalizeClientLocale(previousLocale) == locale &&
		previousSnapshotMode == selectedSnapshotMode {
		configuration, configErr := serverConfiguration(config, pathSet.gamePath)
		if configErr != nil {
			return ServerConfiguration{}, fmt.Errorf("configRead: %w", configErr)
		}
		return configuration, nil
	}
	err = config.Set(game.ConfigServerPort, fmt.Sprintf("%d", port))
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("portSet: %w", err)
	}
	err = config.Set(game.ConfigIsMultiplayerEnabled, strconv.FormatBool(isMultiplayerEnabled))
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("multiplayerSet: %w", err)
	}
	err = config.Set(game.ConfigClientLocale, locale)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("localeSet: %w", err)
	}
	err = config.Set(game.ConfigSnapshotMode, string(selectedSnapshotMode))
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("snapshotModeSet: %w", err)
	}
	err = config.Save(pathSet.configPath)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("configSave: %w", err)
	}

	isNetworkChanged := isSetupChanged || previousPort != port ||
		isPreviouslyMultiplayerEnabled != isMultiplayerEnabled
	if !isNetworkChanged {
		if previousSnapshotMode != selectedSnapshotMode {
			err = gameServer.SetSnapshotMode(string(selectedSnapshotMode))
			if err != nil {
				rollbackErr := config.Set(
					game.ConfigSnapshotMode, string(previousSnapshotMode),
				)
				if rollbackErr == nil {
					rollbackErr = config.Save(pathSet.configPath)
				}
				if rollbackErr != nil {
					return ServerConfiguration{}, fmt.Errorf(
						"snapshotApply: %w; config rollback failed: %v", err, rollbackErr,
					)
				}
				return ServerConfiguration{}, fmt.Errorf("snapshotApply: %w", err)
			}
		}
		return serverConfiguration(config, pathSet.gamePath)
	}
	err = e.restartGameServer(pathSet)
	if err == nil {
		configuration, configErr := serverConfiguration(config, pathSet.gamePath)
		if configErr != nil {
			return ServerConfiguration{}, fmt.Errorf("configRead: %w", configErr)
		}
		return configuration, nil
	}
	restartErr := err
	for key, setting := range previousSettings {
		restoreErr := config.Set(key, setting)
		if restoreErr != nil {
			return ServerConfiguration{}, fmt.Errorf("setupRestore: %w", restoreErr)
		}
	}
	rollbackErr := config.Set(game.ConfigServerPort, fmt.Sprintf("%d", previousPort))
	if rollbackErr == nil {
		rollbackErr = config.Set(
			game.ConfigIsMultiplayerEnabled,
			strconv.FormatBool(isPreviouslyMultiplayerEnabled),
		)
	}
	if rollbackErr == nil {
		rollbackErr = config.Set(game.ConfigClientLocale, previousLocale)
	}
	if rollbackErr == nil {
		rollbackErr = config.Set(
			game.ConfigSnapshotMode, string(previousSnapshotMode),
		)
	}
	if rollbackErr == nil {
		rollbackErr = config.Save(pathSet.configPath)
	}
	if rollbackErr != nil {
		return ServerConfiguration{}, fmt.Errorf(
			"serverRestart: %w; config rollback failed: %v", restartErr, rollbackErr,
		)
	}
	recoveryErr := e.restartGameServer(pathSet)
	if recoveryErr != nil {
		return ServerConfiguration{}, fmt.Errorf(
			"serverRestart: %w; previous configuration recovery failed: %v", restartErr, recoveryErr,
		)
	}
	return ServerConfiguration{}, fmt.Errorf("serverRestart: %w", restartErr)
}

func (e *App) restartGameServer(pathSet *spinnerPathSet) error {
	refreshedPathSet, err := resolveSpinnerPaths(filepath.Dir(pathSet.configPath))
	if err != nil {
		return fmt.Errorf("pathsRefresh: %w", err)
	}
	pathSet = refreshedPathSet
	e.mu.Lock()
	gameServer := e.serviceSet.gameServer
	serverDone := e.serviceSet.serverDone
	e.status.Server = "Restarting"
	e.status.ServerError = ""
	e.status.IsServerOnline = false
	e.status.Auth = "Restarting with server"
	e.status.AuthError = ""
	e.status.IsAuthOnline = false
	e.updatePlayReadyLocked()
	e.emitStatusLocked()
	e.mu.Unlock()
	if gameServer != nil {
		gameServer.Close()
	}
	e.waitForShutdown("Server restart", serverDone)
	e.mu.Lock()
	e.serviceSet.gameServer = nil
	e.serviceSet.serverDone = nil
	ctx := e.lifecycleCtx
	e.mu.Unlock()
	if ctx == nil {
		ctx = context.TODO()
	}
	err = e.prepareContent(ctx, pathSet)
	if err != nil {
		return fmt.Errorf("contentPrepare: %w", err)
	}
	err = e.startGameServer(ctx, pathSet)
	if err != nil {
		e.failSubsystem("Server", fmt.Errorf("restartStart: %w", err))
		return fmt.Errorf("restartStart: %w", err)
	}
	e.setSubsystem("Auth", "Online", true)
	e.mu.Lock()
	e.status.State = "ready"
	e.status.Message = "Server configuration updated"
	e.updatePlayReadyLocked()
	e.emitStatusLocked()
	e.mu.Unlock()
	return nil
}

func (e *App) spinnerPaths() (*spinnerPathSet, error) {
	basePath, err := executableDirectory()
	if err != nil {
		return nil, fmt.Errorf("baseResolve: %w", err)
	}
	pathSet, err := resolveSpinnerPaths(basePath)
	if err != nil {
		return nil, fmt.Errorf("pathResolve: %w", err)
	}
	return pathSet, nil
}

func serverConfiguration(config *game.Config, gamePath string) (ServerConfiguration, error) {
	port, err := config.Uint16(game.ConfigServerPort)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("portLoad: %w", err)
	}
	if port == 0 || port == ^uint16(0) {
		return ServerConfiguration{}, errors.New("server port must be between 1 and 65534")
	}
	locales, err := detectClientLocales(gamePath)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("localeDetect: %w", err)
	}
	locale, err := resolveClientLocale(config.String(game.ConfigClientLocale), locales)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("localeResolve: %w", err)
	}
	snapshotMode, err := snapshot.ParseMode(config.String(game.ConfigSnapshotMode))
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("snapshotModeLoad: %w", err)
	}
	bootstrapWarningMinutes, err := config.Uint32(game.ConfigBootstrapWarningMinutes)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("bootstrapWarningLoad: %w", err)
	}
	bootstrapStartupTimeoutSeconds, err := config.Uint32(game.ConfigBootstrapStartupTimeoutSeconds)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("bootstrapTimeoutLoad: %w", err)
	}
	bootstrapBackupRetentionDays, err := config.Uint32(game.ConfigBootstrapBackupRetentionDays)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("bootstrapRetentionLoad: %w", err)
	}
	return ServerConfiguration{
		IsBootstrapUpdateEnabled:       config.Bool(game.ConfigBootstrapIsUpdateEnabled),
		BootstrapUpdateCron:            config.String(game.ConfigBootstrapUpdateCron),
		BootstrapWarningMinutes:        int(bootstrapWarningMinutes),
		BootstrapStartupTimeoutSeconds: int(bootstrapStartupTimeoutSeconds),
		BootstrapBackupRetentionDays:   int(bootstrapBackupRetentionDays),
		BootstrapManifestURL:           config.String(game.ConfigBootstrapManifestURL),
		BindHost:                       config.String(game.ConfigServerBindHost),
		AdvertisedHost:                 config.String(game.ConfigServerAdvertisedHost),
		IsDedicated:                    config.Bool(game.ConfigServerIsDedicated),
		ContentPath:                    config.String(game.ConfigContentPath),
		JWTSecret:                      config.String(game.ConfigAuthJWTSecret),
		Port:                           port,
		IsMultiplayerEnabled:           config.Bool(game.ConfigIsMultiplayerEnabled),
		Locale:                         locale,
		Locales:                        locales,
		SnapshotMode:                   string(snapshotMode),
	}, nil
}

func validateBootstrapConfiguration(req ServerConfiguration, configPath string) error {
	if req.BootstrapStartupTimeoutSeconds < 1 || req.BootstrapStartupTimeoutSeconds > 86400 {
		return errors.New("bootstrap startup timeout must be between 1 and 86400 seconds")
	}
	if req.BootstrapBackupRetentionDays < 1 || req.BootstrapBackupRetentionDays > 3650 {
		return errors.New("bootstrap backup retention must be between 1 and 3650 days")
	}
	err := maintenance.ValidateSchedule(maintenance.ScheduleOptions{
		Cron:              req.BootstrapUpdateCron,
		WarningMinutes:    req.BootstrapWarningMinutes,
		UpdateRequestPath: filepath.Join(filepath.Dir(configPath), "update-request.json"),
	})
	if err != nil {
		return fmt.Errorf("scheduleValidate: %w", err)
	}
	manifestURL, err := url.Parse(strings.TrimSpace(req.BootstrapManifestURL))
	if err != nil {
		return fmt.Errorf("manifestParse: %w", err)
	}
	if manifestURL.Scheme != "https" || manifestURL.Host == "" || manifestURL.User != nil || manifestURL.Fragment != "" {
		return errors.New("bootstrap manifest URL must use HTTPS without credentials or a fragment")
	}
	return nil
}

func localServerAddress(gameServer *server.Server) (string, error) {
	if gameServer == nil {
		return "", errors.New("local server is not initialized")
	}
	port, err := gameServer.Config().Uint16(game.ConfigServerPort)
	if err != nil {
		return "", fmt.Errorf("serverPort: %w", err)
	}
	return fmt.Sprintf("127.0.0.1:%d", port), nil
}
