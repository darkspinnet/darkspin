package game

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Deployment settings come from the environment. Existing hosts may retain
// their persisted signing key so automatic updates do not rotate credentials.
func dedicatedConfigFromEnvironment(configPath string) (*Config, bool, error) {
	config := DefaultConfig()
	config.values[ConfigServerIsDedicated] = "true"
	config.values[ConfigAuthJWTSecret] = ""
	config.values[ConfigContentPath] = ""
	settings := []struct {
		key        ConfigKey
		variable   string
		isRequired bool
	}{
		{ConfigServerPort, "DARKSPIN_PORT", true},
		{ConfigServerBindHost, "DARKSPIN_BIND_HOST", true},
		{ConfigServerAdvertisedHost, "DARKSPIN_ADVERTISED_HOST", true},
		{ConfigContentPath, "DARKSPIN_CONTENT_PATH", false},
		{ConfigAuthJWTSecret, "DARKSPIN_AUTH_JWT_SECRET", false},
		{ConfigAuthJWTIssuer, "DARKSPIN_AUTH_JWT_ISSUER", false},
		{ConfigAuthJWTAudience, "DARKSPIN_AUTH_JWT_AUDIENCE", false},
		{ConfigIsChatStdoutEnabled, "DARKSPIN_IS_CHAT_STDOUT_ENABLED", false},
		{ConfigIsVersionLocked, "DARKSPIN_IS_VERSION_LOCKED", false},
		{ConfigIsSingleplayerOnly, "DARKSPIN_IS_SINGLEPLAYER_ONLY", false},
		{ConfigSnapshotMode, "DARKSPIN_SNAPSHOT_MODE", false},
		{ConfigSnapshotBufferSecond, "DARKSPIN_SNAPSHOT_BUFFER_SECONDS", false},
		{ConfigSnapshotDelaySecond, "DARKSPIN_SNAPSHOT_DELAY_SECONDS", false},
		{ConfigBootstrapIsUpdateEnabled, "DARKSPIN_IS_UPDATE_ENABLED", false},
		{ConfigBootstrapUpdateCron, "DARKSPIN_UPDATE_CRON", false},
		{ConfigBootstrapWarningMinutes, "DARKSPIN_UPDATE_WARNING_MINUTES", false},
		{ConfigBootstrapStartupTimeoutSeconds, "DARKSPIN_UPDATE_START_TIMEOUT_SECONDS", false},
		{ConfigBootstrapBackupRetentionDays, "DARKSPIN_BACKUP_RETENTION_DAYS", false},
		{ConfigBootstrapManifestURL, "DARKSPIN_UPDATE_MANIFEST_URL", false},
	}
	for _, setting := range settings {
		argument, isPresent := os.LookupEnv(setting.variable)
		if setting.key == ConfigAuthJWTSecret && strings.TrimSpace(argument) == "" {
			continue
		}
		if !isPresent || strings.TrimSpace(argument) == "" {
			if setting.isRequired || isPresent {
				return nil, false, fmt.Errorf("required environment variable %s is empty or missing", setting.variable)
			}
			continue
		}
		config.values[setting.key] = strings.TrimSpace(argument)
	}
	isLegacySigningKey := config.String(ConfigAuthJWTSecret) == ""
	if isLegacySigningKey {
		secret, err := legacyDedicatedSigningKey(configPath)
		if err != nil {
			return nil, false, fmt.Errorf("legacyAuth: %w", err)
		}
		config.values[ConfigAuthJWTSecret] = secret
	}
	if len(config.String(ConfigAuthJWTSecret)) < 32 || config.String(ConfigAuthJWTSecret) == DefaultConfig().String(ConfigAuthJWTSecret) {
		return nil, false, errors.New("set DARKSPIN_AUTH_JWT_SECRET or retain an existing auth.jwt_secret containing at least 32 bytes; the local development secret is not allowed")
	}
	if config.String(ConfigContentPath) == "" {
		if !isLegacySigningKey {
			return nil, false, errors.New("required environment variable DARKSPIN_CONTENT_PATH is missing")
		}
		// Old managed containers did not export a content path. Bootstrap has
		// always kept its cache here; do not consult other legacy TOML settings.
		config.values[ConfigContentPath] = filepath.Join(filepath.Dir(configPath), "darkspin", "cache")
	}
	port, err := config.Uint16(ConfigServerPort)
	if err != nil || port == 0 || port == 65535 {
		return nil, false, errors.New("DARKSPIN_PORT must be between 1 and 65534")
	}
	for _, key := range []ConfigKey{ConfigSnapshotBufferSecond, ConfigSnapshotDelaySecond, ConfigBootstrapWarningMinutes, ConfigBootstrapStartupTimeoutSeconds, ConfigBootstrapBackupRetentionDays} {
		count, parseErr := strconv.ParseUint(config.String(key), 10, 32)
		if parseErr != nil {
			return nil, false, fmt.Errorf("invalid nonnegative integer environment setting for %s", key)
		}
		if (key == ConfigBootstrapStartupTimeoutSeconds || key == ConfigBootstrapBackupRetentionDays) && count == 0 {
			return nil, false, fmt.Errorf("environment setting for %s must be positive", key)
		}
	}
	for _, key := range []ConfigKey{ConfigIsChatStdoutEnabled, ConfigIsVersionLocked, ConfigIsSingleplayerOnly, ConfigBootstrapIsUpdateEnabled} {
		isEnabled, parseErr := strconv.ParseBool(config.String(key))
		if parseErr != nil {
			return nil, false, fmt.Errorf("invalid boolean environment setting for %s", key)
		}
		config.values[key] = strconv.FormatBool(isEnabled)
	}
	bindHost, advertisedHost, err := config.NetworkHosts()
	if err != nil {
		return nil, false, fmt.Errorf("environmentNetwork: %w", err)
	}
	if bindHost == "" || advertisedHost == "" {
		return nil, false, errors.New("dedicated network addresses are required")
	}
	return config, false, nil
}
