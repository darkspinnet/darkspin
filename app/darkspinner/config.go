package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/darkspinnet/darkspin/server/game"
	server "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/darkspinnet/darkspin/server/snapshot"
)

// ServerConfiguration is the launcher-editable server and client configuration.
type ServerConfiguration struct {
	Port                          uint16         `json:"port"`
	IsMultiplayerEnabled          bool           `json:"isMultiplayerEnabled"`
	Locale                        string         `json:"locale"`
	Locales                       []ClientLocale `json:"locales"`
	SnapshotMode                  string         `json:"snapshotMode"`
	IsBorderlessFullscreenEnabled bool           `json:"isBorderlessFullscreenEnabled"`
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
		configuration.IsBorderlessFullscreenEnabled,
	)
}

// SetServerConfiguration persists launcher settings and restarts the embedded
// server only when its network binding changed.
func (e *App) SetServerConfiguration(
	port uint16, isMultiplayerEnabled bool, locale string, snapshotMode string,
	isBorderlessFullscreenEnabled bool,
) (ServerConfiguration, error) {
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
	previousPort, err := config.Uint16(game.ConfigServerPort)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("previousPort: %w", err)
	}
	isPreviouslyMultiplayerEnabled := config.Bool(game.ConfigIsMultiplayerEnabled)
	previousLocale := config.String(game.ConfigClientLocale)
	isPreviouslyBorderlessFullscreenEnabled := config.Bool(game.ConfigIsBorderlessFullscreenEnabled)
	if BuildChannel != "development" {
		isBorderlessFullscreenEnabled = isPreviouslyBorderlessFullscreenEnabled
	}
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
	if previousPort == port && isPreviouslyMultiplayerEnabled == isMultiplayerEnabled &&
		normalizeClientLocale(previousLocale) == locale &&
		previousSnapshotMode == selectedSnapshotMode &&
		isPreviouslyBorderlessFullscreenEnabled == isBorderlessFullscreenEnabled {
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
	err = config.Set(
		game.ConfigIsBorderlessFullscreenEnabled,
		strconv.FormatBool(isBorderlessFullscreenEnabled),
	)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("borderlessSet: %w", err)
	}
	err = config.Save(pathSet.configPath)
	if err != nil {
		return ServerConfiguration{}, fmt.Errorf("configSave: %w", err)
	}

	isNetworkChanged := previousPort != port ||
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
		rollbackErr = config.Set(
			game.ConfigIsBorderlessFullscreenEnabled,
			strconv.FormatBool(isPreviouslyBorderlessFullscreenEnabled),
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
	err := e.startGameServer(ctx, pathSet)
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
	return ServerConfiguration{
		Port:                 port,
		IsMultiplayerEnabled: config.Bool(game.ConfigIsMultiplayerEnabled),
		Locale:               locale,
		Locales:              locales,
		SnapshotMode:         string(snapshotMode),
		IsBorderlessFullscreenEnabled: BuildChannel == "development" &&
			config.Bool(game.ConfigIsBorderlessFullscreenEnabled),
	}, nil
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
