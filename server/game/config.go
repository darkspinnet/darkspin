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

// DefaultConfigFilename is the conventional server configuration filename.
const DefaultConfigFilename = "darkspin.toml"

// ConfigKey identifies a server configuration value.
type ConfigKey string

const (
	ConfigIsVersionLocked                    ConfigKey = "IS_VERSION_LOCKED"
	ConfigIsSingleplayerOnly                 ConfigKey = "IS_SINGLEPLAYER_ONLY"
	ConfigClientLocale                       ConfigKey = "CLIENT_LOCALE"
	ConfigCampaignMatchmakingBotSize         ConfigKey = "CAMPAIGN_MATCHMAKING_BOT_SIZE"
	ConfigIsCampaignBotSquadMirroringEnabled ConfigKey = "IS_CAMPAIGN_BOT_SQUAD_MIRRORING_ENABLED"
	ConfigIsMultiplayerEnabled               ConfigKey = "IS_MULTIPLAYER_ENABLED"
	ConfigServerPort                         ConfigKey = "SERVER_PORT"
	ConfigServerBindHost                     ConfigKey = "SERVER_BIND_HOST"
	ConfigServerAdvertisedHost               ConfigKey = "SERVER_ADVERTISED_HOST"
	ConfigServerIsDedicated                  ConfigKey = "SERVER_IS_DEDICATED"
	ConfigContentPath                        ConfigKey = "CONTENT_PATH"
	ConfigWorldPlayerLimit                   ConfigKey = "WORLD_PLAYER_LIMIT"
	ConfigIsChatStdoutEnabled                ConfigKey = "IS_CHAT_STDOUT_ENABLED"
	ConfigStorageDriver                      ConfigKey = "STORAGE_DRIVER"
	ConfigAuthJWTSecret                      ConfigKey = "AUTH_JWT_SECRET"
	ConfigAuthJWTIssuer                      ConfigKey = "AUTH_JWT_ISSUER"
	ConfigAuthJWTAudience                    ConfigKey = "AUTH_JWT_AUDIENCE"
	ConfigSnapshotMode                       ConfigKey = "SNAPSHOT_MODE"
	ConfigSnapshotBufferSecond               ConfigKey = "SNAPSHOT_BUFFER_SECOND"
	ConfigSnapshotDelaySecond                ConfigKey = "SNAPSHOT_DELAY_SECOND"
	ConfigBootstrapIsUpdateEnabled           ConfigKey = "BOOTSTRAP_IS_UPDATE_ENABLED"
	ConfigBootstrapUpdateCron                ConfigKey = "BOOTSTRAP_UPDATE_CRON"
	ConfigBootstrapWarningMinutes            ConfigKey = "BOOTSTRAP_WARNING_MINUTES"
	ConfigBootstrapStartupTimeoutSeconds     ConfigKey = "BOOTSTRAP_STARTUP_TIMEOUT_SECONDS"
	ConfigBootstrapBackupRetentionDays       ConfigKey = "BOOTSTRAP_BACKUP_RETENTION_DAYS"
	ConfigBootstrapManifestURL               ConfigKey = "BOOTSTRAP_MANIFEST_URL"
)

var defaultConfigValues = map[ConfigKey]string{
	ConfigIsVersionLocked:                    "false",
	ConfigIsSingleplayerOnly:                 "false",
	ConfigClientLocale:                       "",
	ConfigCampaignMatchmakingBotSize:         "1",
	ConfigIsCampaignBotSquadMirroringEnabled: "true",
	ConfigIsMultiplayerEnabled:               "false",
	ConfigServerPort:                         "42127",
	ConfigServerBindHost:                     "",
	ConfigServerAdvertisedHost:               "127.0.0.1",
	ConfigServerIsDedicated:                  "false",
	ConfigContentPath:                        "",
	ConfigWorldPlayerLimit:                   "25",
	ConfigIsChatStdoutEnabled:                "true",
	ConfigStorageDriver:                      "sqlite",
	ConfigAuthJWTSecret:                      "darkspin-local-development-secret-do-not-use-in-production",
	ConfigAuthJWTIssuer:                      "darkspin-web",
	ConfigAuthJWTAudience:                    "darkspin",
	ConfigSnapshotMode:                       "auto",
	ConfigSnapshotBufferSecond:               "60",
	ConfigSnapshotDelaySecond:                "30",
	ConfigBootstrapIsUpdateEnabled:           "true",
	ConfigBootstrapUpdateCron:                "30 3 * * *",
	ConfigBootstrapWarningMinutes:            "30",
	ConfigBootstrapStartupTimeoutSeconds:     "1800",
	ConfigBootstrapBackupRetentionDays:       "7",
	ConfigBootstrapManifestURL:               "https://nightly.link/darkspinnet/darkspin/workflows/main-build/main/darkrun-update-linux-amd64-unstable.zip",
}

// Config is a concurrency-safe representation of darkspin.toml.
type Config struct {
	mu      sync.RWMutex
	values  map[ConfigKey]string
	changes map[ConfigKey]string
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
	IsVersionLocked                    *bool   `toml:"is_version_locked"`
	IsSingleplayerOnly                 *bool   `toml:"is_singleplayer_only"`
	ClientLocale                       *string `toml:"locale"`
	CampaignMatchmakingBotSize         *int64  `toml:"campaign_matchmaking_bot_size"`
	IsCampaignBotSquadMirroringEnabled *bool   `toml:"is_campaign_bot_squad_mirroring_enabled"`
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

	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		isCreated, createErr := createDefaultConfiguration(path)
		if createErr != nil {
			return nil, false, fmt.Errorf("defaultCreate: %w", createErr)
		}
		if !isCreated {
			currentConfig, isCurrentCreated, loadErr := LoadConfig(path)
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

	config.setBool(ConfigIsVersionLocked, document.Game.IsVersionLocked)
	config.setBool(ConfigIsSingleplayerOnly, document.Game.IsSingleplayerOnly)
	config.setString(ConfigClientLocale, document.Game.ClientLocale)
	config.setInt(ConfigCampaignMatchmakingBotSize, document.Game.CampaignMatchmakingBotSize)
	config.setBool(ConfigIsCampaignBotSquadMirroringEnabled, document.Game.IsCampaignBotSquadMirroringEnabled)
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

	_, err = config.CampaignMatchmakingBotSize()
	if err != nil {
		return nil, false, fmt.Errorf("campaignBotSize: %w", err)
	}
	return config, false, nil
}

// Save merges explicitly changed settings into the latest shared document.
// Settings owned by other features retain their current values.
func (e *Config) Save(path string) error {
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
				return fmt.Errorf("unknown configuration field %q", key)
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
	if len(changes) == 0 && len(currentContents) > 0 {
		serverFields, isServerTable := sections["server"].(map[string]any)
		isDedicated, isDedicatedBoolean := serverFields["is_dedicated"].(bool)
		if isServerTable && isDedicatedBoolean && isDedicated {
			// A no-op save must still retain the dedicated signing-key protection
			// enforced by the shared replacement writer on changed documents.
			err = os.Chmod(path, 0o600)
			if err != nil {
				return fmt.Errorf("configProtect: %w", err)
			}
		}
		return nil
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
	ConfigIsVersionLocked:                    {"game", "is_version_locked"},
	ConfigIsSingleplayerOnly:                 {"game", "is_singleplayer_only"},
	ConfigClientLocale:                       {"game", "locale"},
	ConfigCampaignMatchmakingBotSize:         {"game", "campaign_matchmaking_bot_size"},
	ConfigIsCampaignBotSquadMirroringEnabled: {"game", "is_campaign_bot_squad_mirroring_enabled"},
	ConfigServerPort:                         {"server", "port"},
	ConfigServerBindHost:                     {"server", "bind_host"},
	ConfigServerAdvertisedHost:               {"server", "advertised_host"},
	ConfigServerIsDedicated:                  {"server", "is_dedicated"},
	ConfigIsMultiplayerEnabled:               {"server", "is_multiplayer_enabled"},
	ConfigWorldPlayerLimit:                   {"server", "world_player_limit"},
	ConfigContentPath:                        {"content", "path"},
	ConfigIsChatStdoutEnabled:                {"chat", "is_stdout_enabled"},
	ConfigStorageDriver:                      {"storage", "driver"},
	ConfigAuthJWTSecret:                      {"auth", "jwt_secret"},
	ConfigAuthJWTIssuer:                      {"auth", "jwt_issuer"},
	ConfigAuthJWTAudience:                    {"auth", "jwt_audience"},
	ConfigSnapshotMode:                       {"snapshot", "mode"},
	ConfigSnapshotBufferSecond:               {"snapshot", "buffer_duration_seconds"},
	ConfigSnapshotDelaySecond:                {"snapshot", "delay_seconds"},
	ConfigBootstrapIsUpdateEnabled:           {"bootstrap", "is_update_enabled"},
	ConfigBootstrapUpdateCron:                {"bootstrap", "update_cron"},
	ConfigBootstrapWarningMinutes:            {"bootstrap", "warning_minutes"},
	ConfigBootstrapStartupTimeoutSeconds:     {"bootstrap", "startup_timeout_seconds"},
	ConfigBootstrapBackupRetentionDays:       {"bootstrap", "backup_retention_days"},
	ConfigBootstrapManifestURL:               {"bootstrap", "manifest_url"},
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
	campaignMatchmakingBotSize, err := integer(ConfigCampaignMatchmakingBotSize)
	if err != nil {
		return configDocument{}, fmt.Errorf("campaignBotSize: %w", err)
	}
	if *campaignMatchmakingBotSize < 1 || *campaignMatchmakingBotSize > 3 {
		return configDocument{}, errors.New("campaign matchmaking bot size must be between 1 and 3")
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
			IsVersionLocked:                    configPointer(configBool(values[ConfigIsVersionLocked])),
			IsSingleplayerOnly:                 configPointer(configBool(values[ConfigIsSingleplayerOnly])),
			ClientLocale:                       configPointer(values[ConfigClientLocale]),
			CampaignMatchmakingBotSize:         campaignMatchmakingBotSize,
			IsCampaignBotSquadMirroringEnabled: configPointer(configBool(values[ConfigIsCampaignBotSquadMirroringEnabled])),
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

// CampaignMatchmakingBotSize returns the maximum number of campaign AI players.
func (e *Config) CampaignMatchmakingBotSize() (uint32, error) {
	botSize, err := e.Uint32(ConfigCampaignMatchmakingBotSize)
	if err != nil {
		return 0, fmt.Errorf("botSizeParse: %w", err)
	}
	if botSize < 1 || botSize > 3 {
		return 0, errors.New("campaign matchmaking bot size must be between 1 and 3")
	}
	return botSize, nil
}

// Set updates a known configuration value in memory.
func (c *Config) Set(key ConfigKey, value string) error {
	if _, known := defaultConfigValues[key]; !known {
		return fmt.Errorf("unknown config key %q", key)
	}
	if key == ConfigCampaignMatchmakingBotSize {
		botSize, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return fmt.Errorf("botSizeParse: %w", err)
		}
		if botSize < 1 || botSize > 3 {
			return errors.New("campaign matchmaking bot size must be between 1 and 3")
		}
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
