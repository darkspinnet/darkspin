package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/hostconfig"
	hostconfigsql "github.com/darkspinnet/darkspin/server/hostconfig/sqlstore"
)

// LoadLocalConfiguration combines client TOML preferences with database-owned
// server settings, preserving existing local settings on the first database load.
func LoadLocalConfiguration(configFilename string) (*game.Config, bool, error) {
	config, isCreated, err := LoadLocalConfigurationAt(configFilename, localHostDatabasePath(configFilename))
	if err != nil {
		return nil, false, fmt.Errorf("localLoad: %w", err)
	}
	return config, isCreated, nil
}

// LoadLocalConfigurationAt keeps isolated hosts in their own profile database.
func LoadLocalConfigurationAt(configFilename, databasePath string) (*game.Config, bool, error) {
	configPath, err := filepath.Abs(configFilename)
	if err != nil {
		return nil, false, fmt.Errorf("configPath: %w", err)
	}
	seed, err := readLegacyLocalConfiguration(configPath)
	if err != nil {
		return nil, false, fmt.Errorf("legacyRead: %w", err)
	}
	config, isCreated, err := game.LoadConfig(configPath)
	if err != nil {
		return nil, false, fmt.Errorf("clientLoad: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, closer, err := openLocalHostConfiguration(ctx, databasePath, seed)
	if err != nil {
		return nil, false, fmt.Errorf("hostOpen: %w", err)
	}
	settings, readErr := store.Read(ctx)
	closeErr := closer.Close()
	if readErr != nil || closeErr != nil {
		return nil, false, fmt.Errorf("hostRead: %w", errors.Join(readErr, closeErr))
	}
	err = applyLocalHostConfiguration(config, settings)
	if err != nil {
		return nil, false, fmt.Errorf("hostApply: %w", err)
	}
	err = config.RetireServerConfiguration(configPath)
	if err != nil {
		return nil, false, fmt.Errorf("legacyRetire: %w", err)
	}
	return config, isCreated, nil
}

// SaveLocalConfiguration stores server settings in the database and delegates
// only client preferences to the shared TOML writer.
func SaveLocalConfiguration(configFilename string, config *game.Config) error {
	err := SaveLocalConfigurationAt(configFilename, localHostDatabasePath(configFilename), config)
	if err != nil {
		return fmt.Errorf("localSave: %w", err)
	}
	return nil
}

// SaveLocalConfigurationAt persists the settings of an isolated local host.
func SaveLocalConfigurationAt(configFilename, databasePath string, config *game.Config) error {
	if config == nil || config.Bool(game.ConfigServerIsDedicated) {
		return errors.New("local configuration required")
	}
	settings, err := localHostConfiguration(config)
	if err != nil {
		return fmt.Errorf("hostSettings: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	defaults, err := readLegacyLocalConfiguration(configFilename)
	if err != nil {
		return fmt.Errorf("defaultsRead: %w", err)
	}
	store, closer, err := openLocalHostConfiguration(ctx, databasePath, defaults)
	if err != nil {
		return fmt.Errorf("hostOpen: %w", err)
	}
	previous, readErr := store.Read(ctx)
	if readErr != nil {
		closeErr := closer.Close()
		return fmt.Errorf("hostRead: %w", errors.Join(readErr, closeErr))
	}
	saveErr := store.Save(ctx, settings)
	if saveErr != nil {
		closeErr := closer.Close()
		return fmt.Errorf("hostSave: %w", errors.Join(saveErr, closeErr))
	}
	clientErr := config.RetireServerConfiguration(configFilename)
	if clientErr == nil {
		clientErr = config.Save(configFilename)
	}
	if clientErr != nil {
		rollbackErr := store.Save(ctx, previous)
		closeErr := closer.Close()
		return fmt.Errorf("clientSave: %w", errors.Join(clientErr, rollbackErr, closeErr))
	}
	closeErr := closer.Close()
	if closeErr != nil {
		return fmt.Errorf("hostClose: %w", closeErr)
	}
	return nil
}

func localHostDatabasePath(configFilename string) string {
	return filepath.Join(filepath.Dir(configFilename), DarkspinDirectory, SaveDirectory, UserDatabaseFilename)
}

func openLocalHostConfiguration(ctx context.Context, databaseFilename string, seed hostconfig.Settings) (*hostconfigsql.Store, io.Closer, error) {
	databasePath, err := filepath.Abs(databaseFilename)
	if err != nil {
		return nil, nil, fmt.Errorf("databasePath: %w", err)
	}
	defaults := game.DefaultConfig()
	repository, closer, err := openUserRepository(defaults, databasePath)
	if err != nil {
		return nil, nil, fmt.Errorf("profileOpen: %w", err)
	}
	provider, isSupported := repository.(interface{ HostConfigurationStore() *hostconfigsql.Store })
	if !isSupported {
		closeErr := closer.Close()
		return nil, nil, fmt.Errorf("hostPort: %w", errors.Join(errors.New("host configuration storage unavailable"), closeErr))
	}
	store := provider.HostConfigurationStore()
	err = store.EnsureSchema(ctx, seed)
	if err != nil {
		closeErr := closer.Close()
		return nil, nil, fmt.Errorf("hostInit: %w", errors.Join(err, closeErr))
	}
	return store, closer, nil
}

func localHostConfiguration(config *game.Config) (hostconfig.Settings, error) {
	port, err := config.Uint16(game.ConfigServerPort)
	if err != nil {
		return hostconfig.Settings{}, fmt.Errorf("portRead: %w", err)
	}
	settings := hostconfig.Settings{
		Port: port, BindHost: config.String(game.ConfigServerBindHost), AdvertisedHost: config.String(game.ConfigServerAdvertisedHost),
		IsVersionLocked: config.Bool(game.ConfigIsVersionLocked), IsSingleplayerOnly: config.Bool(game.ConfigIsSingleplayerOnly),
		IsChatStdoutEnabled: config.Bool(game.ConfigIsChatStdoutEnabled), JWTSecret: config.String(game.ConfigAuthJWTSecret),
		JWTIssuer: config.String(game.ConfigAuthJWTIssuer), JWTAudience: config.String(game.ConfigAuthJWTAudience), ContentPath: config.String(game.ConfigContentPath),
	}
	err = hostconfig.Validate(settings)
	if err != nil {
		return hostconfig.Settings{}, fmt.Errorf("hostValidate: %w", err)
	}
	return settings, nil
}

func applyLocalHostConfiguration(config *game.Config, req hostconfig.Settings) error {
	settings := map[game.ConfigKey]string{
		game.ConfigServerPort: strconv.FormatUint(uint64(req.Port), 10), game.ConfigServerBindHost: req.BindHost, game.ConfigServerAdvertisedHost: req.AdvertisedHost,
		game.ConfigIsVersionLocked:    strconv.FormatBool(req.IsVersionLocked),
		game.ConfigIsSingleplayerOnly: strconv.FormatBool(req.IsSingleplayerOnly), game.ConfigIsChatStdoutEnabled: strconv.FormatBool(req.IsChatStdoutEnabled),
		game.ConfigAuthJWTSecret: req.JWTSecret, game.ConfigAuthJWTIssuer: req.JWTIssuer, game.ConfigAuthJWTAudience: req.JWTAudience, game.ConfigContentPath: req.ContentPath,
	}
	for key, setting := range settings {
		err := config.Set(key, setting)
		if err != nil {
			return fmt.Errorf("hostSet[%s]: %w", key, err)
		}
	}
	return nil
}
