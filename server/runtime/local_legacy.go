package runtime

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/hostconfig"
)

// readLegacyLocalConfiguration preserves settings only when initializing the
// local database. Mode flags and bot preferences deliberately have no fields.
func readLegacyLocalConfiguration(configPath string) (hostconfig.Settings, error) {
	settings, err := localHostConfiguration(game.DefaultConfig())
	if err != nil {
		return hostconfig.Settings{}, fmt.Errorf("defaultsRead: %w", err)
	}
	contents, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return hostconfig.Settings{}, fmt.Errorf("legacyRead: %w", err)
	}
	var document struct {
		Server struct {
			Port *uint16 `toml:"port"`
		} `toml:"server"`
		Game struct {
			IsVersionLocked    *bool `toml:"is_version_locked"`
			IsSingleplayerOnly *bool `toml:"is_singleplayer_only"`
		} `toml:"game"`
		Chat struct {
			IsStdoutEnabled *bool `toml:"is_stdout_enabled"`
		} `toml:"chat"`
		Auth struct {
			JWTSecret   *string `toml:"jwt_secret"`
			JWTIssuer   *string `toml:"jwt_issuer"`
			JWTAudience *string `toml:"jwt_audience"`
		} `toml:"auth"`
		Content struct {
			Path *string `toml:"path"`
		} `toml:"content"`
	}
	err = toml.Unmarshal(contents, &document)
	if err != nil {
		return hostconfig.Settings{}, fmt.Errorf("legacyDecode: %w", err)
	}
	if document.Server.Port != nil {
		settings.Port = *document.Server.Port
	}
	if document.Game.IsVersionLocked != nil {
		settings.IsVersionLocked = *document.Game.IsVersionLocked
	}
	if document.Game.IsSingleplayerOnly != nil {
		settings.IsSingleplayerOnly = *document.Game.IsSingleplayerOnly
	}
	if document.Chat.IsStdoutEnabled != nil {
		settings.IsChatStdoutEnabled = *document.Chat.IsStdoutEnabled
	}
	if document.Auth.JWTSecret != nil {
		settings.JWTSecret = *document.Auth.JWTSecret
	}
	if document.Auth.JWTIssuer != nil {
		settings.JWTIssuer = *document.Auth.JWTIssuer
	}
	if document.Auth.JWTAudience != nil {
		settings.JWTAudience = *document.Auth.JWTAudience
	}
	if document.Content.Path != nil {
		settings.ContentPath = *document.Content.Path
	}
	err = hostconfig.Validate(settings)
	if err != nil {
		return hostconfig.Settings{}, fmt.Errorf("legacyValidate: %w", err)
	}
	return settings, nil
}
