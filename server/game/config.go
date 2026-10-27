// Package game contains Game game services and domain configuration.
package game

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/darkspinnet/darkspin/server/assets"
)

// DefaultConfigFilename is the conventional server configuration filename.
const DefaultConfigFilename = "darkspin.toml"

// ConfigKey identifies a server configuration value.
type ConfigKey string

const (
	ConfigIsVersionLocked                ConfigKey = "IS_VERSION_LOCKED"
	ConfigIsSingleplayerOnly             ConfigKey = "IS_SINGLEPLAYER_ONLY"
	ConfigClientLocale                   ConfigKey = "CLIENT_LOCALE"
	ConfigIsMultiplayerEnabled           ConfigKey = "IS_MULTIPLAYER_ENABLED"
	ConfigServerPort                     ConfigKey = "SERVER_PORT"
	ConfigServerBindHost                 ConfigKey = "SERVER_BIND_HOST"
	ConfigServerAdvertisedHost           ConfigKey = "SERVER_ADVERTISED_HOST"
	ConfigServerIsDedicated              ConfigKey = "SERVER_IS_DEDICATED"
	ConfigContentPath                    ConfigKey = "CONTENT_PATH"
	ConfigWorldPlayerLimit               ConfigKey = "WORLD_PLAYER_LIMIT"
	ConfigIsChatStdoutEnabled            ConfigKey = "IS_CHAT_STDOUT_ENABLED"
	ConfigStorageDriver                  ConfigKey = "STORAGE_DRIVER"
	ConfigAuthJWTSecret                  ConfigKey = "AUTH_JWT_SECRET"
	ConfigAuthJWTIssuer                  ConfigKey = "AUTH_JWT_ISSUER"
	ConfigAuthJWTAudience                ConfigKey = "AUTH_JWT_AUDIENCE"
	ConfigSnapshotMode                   ConfigKey = "SNAPSHOT_MODE"
	ConfigSnapshotBufferSecond           ConfigKey = "SNAPSHOT_BUFFER_SECOND"
	ConfigSnapshotDelaySecond            ConfigKey = "SNAPSHOT_DELAY_SECOND"
	ConfigBootstrapIsUpdateEnabled       ConfigKey = "BOOTSTRAP_IS_UPDATE_ENABLED"
	ConfigBootstrapUpdateCron            ConfigKey = "BOOTSTRAP_UPDATE_CRON"
	ConfigBootstrapWarningMinutes        ConfigKey = "BOOTSTRAP_WARNING_MINUTES"
	ConfigBootstrapStartupTimeoutSeconds ConfigKey = "BOOTSTRAP_STARTUP_TIMEOUT_SECONDS"
	ConfigBootstrapBackupRetentionDays   ConfigKey = "BOOTSTRAP_BACKUP_RETENTION_DAYS"
	ConfigBootstrapManifestURL           ConfigKey = "BOOTSTRAP_MANIFEST_URL"
)

var defaultConfigValues = map[ConfigKey]string{
	ConfigIsVersionLocked:                "false",
	ConfigIsSingleplayerOnly:             "false",
	ConfigClientLocale:                   "",
	ConfigIsMultiplayerEnabled:           "false",
	ConfigServerPort:                     "42127",
	ConfigServerBindHost:                 "",
	ConfigServerAdvertisedHost:           "127.0.0.1",
	ConfigServerIsDedicated:              "false",
	ConfigContentPath:                    "",
	ConfigWorldPlayerLimit:               "25",
	ConfigIsChatStdoutEnabled:            "true",
	ConfigStorageDriver:                  "sqlite",
	ConfigAuthJWTSecret:                  "darkspin-local-development-secret-do-not-use-in-production",
	ConfigAuthJWTIssuer:                  "darkspin-web",
	ConfigAuthJWTAudience:                "darkspin",
	ConfigSnapshotMode:                   "auto",
	ConfigSnapshotBufferSecond:           "60",
	ConfigSnapshotDelaySecond:            "30",
	ConfigBootstrapIsUpdateEnabled:       "true",
	ConfigBootstrapUpdateCron:            "30 3 * * *",
	ConfigBootstrapWarningMinutes:        "30",
	ConfigBootstrapStartupTimeoutSeconds: "1800",
	ConfigBootstrapBackupRetentionDays:   "7",
	ConfigBootstrapManifestURL:           "https://nightly.link/darkspinnet/darkspin/workflows/main-build/main/darkrun-update-linux-amd64-unstable.zip",
}

// Config is a concurrency-safe representation of darkspin.toml.
type Config struct {
	mu     sync.RWMutex
	values map[ConfigKey]string
}

type configDocument struct {
	LauncherFields map[string]any  `toml:"launcher,omitempty"`
	Game           configGame      `toml:"game"`
	Server         configServer    `toml:"server"`
	Chat           configChat      `toml:"chat"`
	Storage        configStorage   `toml:"storage"`
	Auth           configAuth      `toml:"auth"`
	Snapshot       configSnapshot  `toml:"snapshot"`
	Content        configContent   `toml:"content"`
	Bootstrap      configBootstrap `toml:"bootstrap"`
}

type configBootstrap struct {
	IsUpdateEnabled       *bool   `toml:"is_update_enabled"`
	UpdateCron            *string `toml:"update_cron"`
	WarningMinutes        *int64  `toml:"warning_minutes"`
	StartupTimeoutSeconds *int64  `toml:"startup_timeout_seconds"`
	BackupRetentionDays   *int64  `toml:"backup_retention_days"`
	ManifestURL           *string `toml:"manifest_url"`
}

type configContent struct {
	Path *string `toml:"path"`
}

type configAuth struct {
	JWTSecret   *string `toml:"jwt_secret"`
	JWTIssuer   *string `toml:"jwt_issuer"`
	JWTAudience *string `toml:"jwt_audience"`
}

type configChat struct {
	IsStdoutEnabled *bool `toml:"is_stdout_enabled"`
}

type configGame struct {
	IsVersionLocked    *bool   `toml:"is_version_locked"`
	IsSingleplayerOnly *bool   `toml:"is_singleplayer_only"`
	ClientLocale       *string `toml:"locale"`
}

type configServer struct {
	BindHost             *string `toml:"bind_host"`
	AdvertisedHost       *string `toml:"advertised_host"`
	IsDedicated          *bool   `toml:"is_dedicated"`
	IsMultiplayerEnabled *bool   `toml:"is_multiplayer_enabled"`
	Port                 *int64  `toml:"port"`
	WorldPlayerLimit     *int64  `toml:"world_player_limit"`
}

type configStorage struct {
	Driver *string `toml:"driver"`
}

type configSnapshot struct {
	Mode                 *string `toml:"mode"`
	BufferDurationSecond *int64  `toml:"buffer_duration_seconds"`
	DelaySecond          *int64  `toml:"delay_seconds"`
}

// DefaultConfig returns an independent configuration populated with darkspin's
// local all-in-one defaults.
func DefaultConfig() *Config {
	values := make(map[ConfigKey]string, len(defaultConfigValues))
	for key, value := range defaultConfigValues {
		values[key] = value
	}
	return &Config{values: values}
}

// LoadConfig reads a TOML configuration. When the file does not exist, it
// writes and returns the default configuration.
func LoadConfig(path string) (*Config, bool, error) {
	config := DefaultConfig()

	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		directory := filepath.Dir(path)
		err = os.MkdirAll(directory, 0o755)
		if err != nil {
			return nil, false, fmt.Errorf("defaultDirectory: %w", err)
		}
		err = os.WriteFile(path, assets.DefaultConfigTOML, 0o644)
		if err != nil {
			return nil, false, fmt.Errorf("defaultSave: %w", err)
		}
		return config, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("configOpen: %w", err)
	}
	defer file.Close()

	document := configDocument{}
	_, err = toml.NewDecoder(file).Decode(&document)
	if err != nil {
		return nil, false, fmt.Errorf("configDecode: %w", err)
	}

	config.setBool(ConfigIsVersionLocked, document.Game.IsVersionLocked)
	config.setBool(ConfigIsSingleplayerOnly, document.Game.IsSingleplayerOnly)
	config.setString(ConfigClientLocale, document.Game.ClientLocale)
	config.setBool(ConfigIsMultiplayerEnabled, document.Server.IsMultiplayerEnabled)
	config.setInt(ConfigServerPort, document.Server.Port)
	config.setString(ConfigServerBindHost, document.Server.BindHost)
	config.setString(ConfigServerAdvertisedHost, document.Server.AdvertisedHost)
	config.setBool(ConfigServerIsDedicated, document.Server.IsDedicated)
	config.setString(ConfigContentPath, document.Content.Path)
	config.setInt(ConfigWorldPlayerLimit, document.Server.WorldPlayerLimit)
	config.setBool(ConfigIsChatStdoutEnabled, document.Chat.IsStdoutEnabled)
	config.setString(ConfigStorageDriver, document.Storage.Driver)
	config.setString(ConfigAuthJWTSecret, document.Auth.JWTSecret)
	config.setString(ConfigAuthJWTIssuer, document.Auth.JWTIssuer)
	config.setString(ConfigAuthJWTAudience, document.Auth.JWTAudience)
	config.setString(ConfigSnapshotMode, document.Snapshot.Mode)
	config.setInt(ConfigSnapshotBufferSecond, document.Snapshot.BufferDurationSecond)
	config.setInt(ConfigSnapshotDelaySecond, document.Snapshot.DelaySecond)
	config.setBool(ConfigBootstrapIsUpdateEnabled, document.Bootstrap.IsUpdateEnabled)
	config.setString(ConfigBootstrapUpdateCron, document.Bootstrap.UpdateCron)
	config.setInt(ConfigBootstrapWarningMinutes, document.Bootstrap.WarningMinutes)
	config.setInt(ConfigBootstrapStartupTimeoutSeconds, document.Bootstrap.StartupTimeoutSeconds)
	config.setInt(ConfigBootstrapBackupRetentionDays, document.Bootstrap.BackupRetentionDays)
	config.setString(ConfigBootstrapManifestURL, document.Bootstrap.ManifestURL)

	return config, false, nil
}

// Save writes a compact, typed TOML configuration.
func (c *Config) Save(path string) error {
	c.mu.RLock()
	values := make(map[ConfigKey]string, len(c.values))
	for key, value := range c.values {
		values[key] = value
	}
	c.mu.RUnlock()

	document, err := encodeConfigDocument(values)
	if err != nil {
		return fmt.Errorf("configEncode: %w", err)
	}
	// Launcher-owned settings are read at save time so an older server Config
	// cannot overwrite a launcher preference changed since the server started.
	currentContents, readErr := os.ReadFile(path)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("launcherRead: %w", readErr)
	}
	if readErr == nil {
		var currentDocument configDocument
		decodeErr := toml.Unmarshal(currentContents, &currentDocument)
		if decodeErr != nil {
			return fmt.Errorf("launcherDecode: %w", decodeErr)
		}
		document.LauncherFields = currentDocument.LauncherFields
	}
	contents := bytes.Buffer{}
	err = toml.NewEncoder(&contents).Encode(document)
	if err != nil {
		return fmt.Errorf("tomlEncode: %w", err)
	}

	mode := os.FileMode(0o644)
	if configBool(values[ConfigServerIsDedicated]) {
		mode = 0o600
		// Tighten an existing file before writing the dedicated signing key.
		// WriteFile only applies its mode when creating a new file.
		err = os.Chmod(path, mode)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("configProtect: %w", err)
		}
	}
	err = os.WriteFile(path, contents.Bytes(), mode)
	if err != nil {
		return fmt.Errorf("configWrite: %w", err)
	}
	return nil
}

func encodeConfigDocument(values map[ConfigKey]string) (configDocument, error) {
	integer := func(key ConfigKey) (*int64, error) {
		value, err := strconv.ParseInt(values[key], 0, 64)
		if err != nil {
			return nil, fmt.Errorf("integer[%s]: %w", key, err)
		}
		return configPointer(value), nil
	}

	serverPort, err := integer(ConfigServerPort)
	if err != nil {
		return configDocument{}, fmt.Errorf("serverPort: %w", err)
	}
	worldPlayerLimit, err := integer(ConfigWorldPlayerLimit)
	if err != nil {
		return configDocument{}, fmt.Errorf("worldPlayerLimit: %w", err)
	}
	snapshotBufferSecond, err := integer(ConfigSnapshotBufferSecond)
	if err != nil {
		return configDocument{}, fmt.Errorf("snapshotBuffer: %w", err)
	}
	snapshotDelaySecond, err := integer(ConfigSnapshotDelaySecond)
	if err != nil {
		return configDocument{}, fmt.Errorf("snapshotDelay: %w", err)
	}
	bootstrapWarningMinutes, err := integer(ConfigBootstrapWarningMinutes)
	if err != nil {
		return configDocument{}, fmt.Errorf("bootstrapWarning: %w", err)
	}
	bootstrapStartupTimeoutSeconds, err := integer(ConfigBootstrapStartupTimeoutSeconds)
	if err != nil {
		return configDocument{}, fmt.Errorf("bootstrapTimeout: %w", err)
	}
	bootstrapBackupRetentionDays, err := integer(ConfigBootstrapBackupRetentionDays)
	if err != nil {
		return configDocument{}, fmt.Errorf("bootstrapRetention: %w", err)
	}
	return configDocument{
		Bootstrap: configBootstrap{
			IsUpdateEnabled:       configPointer(configBool(values[ConfigBootstrapIsUpdateEnabled])),
			UpdateCron:            configPointer(values[ConfigBootstrapUpdateCron]),
			WarningMinutes:        bootstrapWarningMinutes,
			StartupTimeoutSeconds: bootstrapStartupTimeoutSeconds,
			BackupRetentionDays:   bootstrapBackupRetentionDays,
			ManifestURL:           configPointer(values[ConfigBootstrapManifestURL]),
		},
		Content: configContent{Path: configPointer(values[ConfigContentPath])},
		Game: configGame{
			IsVersionLocked:    configPointer(configBool(values[ConfigIsVersionLocked])),
			IsSingleplayerOnly: configPointer(configBool(values[ConfigIsSingleplayerOnly])),
			ClientLocale:       configPointer(values[ConfigClientLocale]),
		},
		Server: configServer{
			BindHost:             configPointer(values[ConfigServerBindHost]),
			AdvertisedHost:       configPointer(values[ConfigServerAdvertisedHost]),
			IsDedicated:          configPointer(configBool(values[ConfigServerIsDedicated])),
			IsMultiplayerEnabled: configPointer(configBool(values[ConfigIsMultiplayerEnabled])),
			Port:                 serverPort,
			WorldPlayerLimit:     worldPlayerLimit,
		},
		Chat: configChat{
			IsStdoutEnabled: configPointer(configBool(values[ConfigIsChatStdoutEnabled])),
		},
		Storage: configStorage{
			Driver: configPointer(values[ConfigStorageDriver]),
		},
		Auth: configAuth{
			JWTSecret:   configPointer(values[ConfigAuthJWTSecret]),
			JWTIssuer:   configPointer(values[ConfigAuthJWTIssuer]),
			JWTAudience: configPointer(values[ConfigAuthJWTAudience]),
		},
		Snapshot: configSnapshot{
			Mode:                 configPointer(values[ConfigSnapshotMode]),
			BufferDurationSecond: snapshotBufferSecond,
			DelaySecond:          snapshotDelaySecond,
		},
	}, nil
}

func configPointer[T any](value T) *T {
	return &value
}

func configBool(value string) bool {
	return value == "1" || strings.EqualFold(value, "true")
}

func (c *Config) setBool(key ConfigKey, isEnabled *bool) {
	if isEnabled != nil {
		c.values[key] = strconv.FormatBool(*isEnabled)
	}
}

func (c *Config) setInt(key ConfigKey, value *int64) {
	if value != nil {
		c.values[key] = strconv.FormatInt(*value, 10)
	}
}

func (c *Config) setString(key ConfigKey, value *string) {
	if value == nil {
		return
	}
	c.values[key] = *value
}

// String returns a configuration value, or an empty string for an unknown key.
func (c *Config) String(key ConfigKey) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.values[key]
}

// Bool matches recap_server's boolean parsing: "1" and case-insensitive
// "true" are true; all other values are false.
func (c *Config) Bool(key ConfigKey) bool {
	value := c.String(key)
	return value == "1" || strings.EqualFold(value, "true")
}

// Uint16 parses a configuration value as an unsigned 16-bit integer.
func (c *Config) Uint16(key ConfigKey) (uint16, error) {
	parsed, err := c.parseUint(key, 16)
	if err != nil {
		return 0, fmt.Errorf("uint16[%q]: %w", key, err)
	}
	return uint16(parsed), nil
}

// Uint32 parses a configuration value as an unsigned 32-bit integer.
func (c *Config) Uint32(key ConfigKey) (uint32, error) {
	parsed, err := c.parseUint(key, 32)
	if err != nil {
		return 0, fmt.Errorf("uint32[%q]: %w", key, err)
	}
	return uint32(parsed), nil
}

// Set updates a known configuration value in memory.
func (c *Config) Set(key ConfigKey, value string) error {
	if _, known := defaultConfigValues[key]; !known {
		return fmt.Errorf("unknown config key %q", key)
	}
	c.mu.Lock()
	c.values[key] = value
	c.mu.Unlock()
	return nil
}

func (c *Config) parseUint(key ConfigKey, bitSize int) (uint64, error) {
	value := c.String(key)
	parsed, err := strconv.ParseUint(value, 0, bitSize)
	if err != nil {
		return 0, fmt.Errorf("uintParse[%s/%d]: %w", key, bitSize, err)
	}
	return parsed, nil
}
