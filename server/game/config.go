// Package game contains Game game services and domain configuration.
package game

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

// DefaultConfigFilename is the client preference filename and legacy runtime anchor.
const DefaultConfigFilename = "darkspin.toml"

// ConfigKey identifies a server configuration value.
type ConfigKey string

const (
	ConfigIsVersionLocked                ConfigKey = "IS_VERSION_LOCKED"
	ConfigIsSingleplayerOnly             ConfigKey = "IS_SINGLEPLAYER_ONLY"
	ConfigClientLocale                   ConfigKey = "CLIENT_LOCALE"
	ConfigServerPort                     ConfigKey = "SERVER_PORT"
	ConfigServerBindHost                 ConfigKey = "SERVER_BIND_HOST"
	ConfigServerAdvertisedHost           ConfigKey = "SERVER_ADVERTISED_HOST"
	ConfigServerIsDedicated              ConfigKey = "SERVER_IS_DEDICATED"
	ConfigContentPath                    ConfigKey = "CONTENT_PATH"
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
	ConfigServerPort:                     "42127",
	ConfigServerBindHost:                 "",
	ConfigServerAdvertisedHost:           "127.0.0.1",
	ConfigServerIsDedicated:              "false",
	ConfigContentPath:                    "",
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

// Config combines host-owned runtime settings with client preferences.
type Config struct {
	mu      sync.RWMutex
	values  map[ConfigKey]string
	changes map[ConfigKey]string
}

type configDocument struct {
	LauncherFields map[string]any `toml:"launcher,omitempty"`
	Game           configGame     `toml:"game"`
	Snapshot       configSnapshot `toml:"snapshot"`
}

type configGame struct {
	ClientLocale *string `toml:"locale"`
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
	return loadConfig(path)
}

// LoadDedicatedConfig is reserved for dedicated hosts and their administration
// entry points. Only the legacy signing key may fall back to this path.
func LoadDedicatedConfig(path string) (*Config, bool, error) {
	return dedicatedConfigFromEnvironment(path)
}

func loadConfig(path string) (*Config, bool, error) {
	config := DefaultConfig()

	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		isCreated, createErr := createDefaultConfiguration(path)
		if createErr != nil {
			return nil, false, fmt.Errorf("defaultCreate: %w", createErr)
		}
		if !isCreated {
			currentConfig, isCurrentCreated, loadErr := loadConfig(path)
			if loadErr != nil {
				return nil, false, fmt.Errorf("defaultReload: %w", loadErr)
			}
			return currentConfig, isCurrentCreated, nil
		}
		return config, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("configOpen: %w", err)
	}
	document := configDocument{}
	err = toml.Unmarshal(contents, &document)
	if err != nil {
		return nil, false, fmt.Errorf("configDecode: %w", err)
	}
	config.setString(ConfigClientLocale, document.Game.ClientLocale)
	config.setString(ConfigSnapshotMode, document.Snapshot.Mode)
	config.setInt(ConfigSnapshotBufferSecond, document.Snapshot.BufferDurationSecond)
	config.setInt(ConfigSnapshotDelaySecond, document.Snapshot.DelaySecond)

	return config, false, nil
}

// Save merges explicitly changed settings into the latest shared document.
// Settings owned by other features retain their current values.
func (e *Config) Save(path string) error {
	if e.Bool(ConfigServerIsDedicated) {
		return errors.New("dedicated configuration is environment-only")
	}
	e.mu.RLock()
	settings := make(map[ConfigKey]string, len(e.values))
	for key, setting := range e.values {
		settings[key] = setting
	}
	changes := make(map[ConfigKey]string, len(e.changes))
	for key, setting := range e.changes {
		changes[key] = setting
	}
	e.mu.RUnlock()
	document, err := encodeConfigDocument(settings)
	if err != nil {
		return fmt.Errorf("configEncode: %w", err)
	}
	var typedContents bytes.Buffer
	err = toml.NewEncoder(&typedContents).Encode(document)
	if err != nil {
		return fmt.Errorf("configMarshal: %w", err)
	}
	typedSections := make(map[string]any)
	err = toml.Unmarshal(typedContents.Bytes(), &typedSections)
	if err != nil {
		return fmt.Errorf("configProjection: %w", err)
	}
	configurationMutex.Lock()
	defer configurationMutex.Unlock()
	currentContents, err := readConfiguration(path)
	if err != nil {
		return fmt.Errorf("configRead: %w", err)
	}
	sections := make(map[string]any)
	err = toml.Unmarshal(currentContents, &sections)
	if err != nil {
		return fmt.Errorf("configDecode: %w", err)
	}
	if len(sections) == 0 {
		sections = typedSections
	} else {
		for key := range changes {
			fieldPath, isKnown := configFieldPaths[key]
			if !isKnown {
				continue
			}
			typedFields, isTypedTable := typedSections[fieldPath[0]].(map[string]any)
			if !isTypedTable {
				return fmt.Errorf("invalid configuration section %q", fieldPath[0])
			}
			fields, isTable := sections[fieldPath[0]].(map[string]any)
			if !isTable {
				fields = make(map[string]any)
				sections[fieldPath[0]] = fields
			}
			fields[fieldPath[1]] = typedFields[fieldPath[1]]
		}
	}
	var contents bytes.Buffer
	err = toml.NewEncoder(&contents).Encode(sections)
	if err != nil {
		return fmt.Errorf("tomlEncode: %w", err)
	}
	err = replaceConfiguration(path, currentContents, contents.Bytes())
	if err != nil {
		return fmt.Errorf("configWrite: %w", err)
	}
	e.mu.Lock()
	for key, setting := range changes {
		if e.changes[key] == setting {
			delete(e.changes, key)
		}
	}
	e.mu.Unlock()
	return nil
}

var configFieldPaths = map[ConfigKey][2]string{
	ConfigClientLocale:         {"game", "locale"},
	ConfigSnapshotMode:         {"snapshot", "mode"},
	ConfigSnapshotBufferSecond: {"snapshot", "buffer_duration_seconds"},
	ConfigSnapshotDelaySecond:  {"snapshot", "delay_seconds"},
}

func encodeConfigDocument(values map[ConfigKey]string) (configDocument, error) {
	integer := func(key ConfigKey) (*int64, error) {
		value, err := strconv.ParseInt(values[key], 0, 64)
		if err != nil {
			return nil, fmt.Errorf("integer[%s]: %w", key, err)
		}
		return configPointer(value), nil
	}

	snapshotBufferSecond, err := integer(ConfigSnapshotBufferSecond)
	if err != nil {
		return configDocument{}, fmt.Errorf("snapshotBuffer: %w", err)
	}
	snapshotDelaySecond, err := integer(ConfigSnapshotDelaySecond)
	if err != nil {
		return configDocument{}, fmt.Errorf("snapshotDelay: %w", err)
	}
	return configDocument{
		Game: configGame{ClientLocale: configPointer(values[ConfigClientLocale])},
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
	if key == ConfigServerIsDedicated {
		return errors.New("server mode is selected by the host entry point")
	}
	if _, known := defaultConfigValues[key]; !known {
		return fmt.Errorf("unknown config key %q", key)
	}
	c.mu.Lock()
	c.values[key] = value
	if c.changes == nil {
		c.changes = make(map[ConfigKey]string)
	}
	c.changes[key] = value
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
