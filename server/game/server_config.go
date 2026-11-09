package game

import (
	"bytes"
	"fmt"

	"github.com/BurntSushi/toml"
)

// RetireServerConfiguration removes old local server fields after their
// database replacement is durable. Dedicated hosts never touch this file.
func (e *Config) RetireServerConfiguration(path string) error {
	if e.Bool(ConfigServerIsDedicated) {
		return nil
	}
	err := WithConfiguration(path, removeServerConfiguration)
	if err != nil {
		return fmt.Errorf("serverRetire: %w", err)
	}
	return nil
}

func removeServerConfiguration(contents []byte) ([]byte, error) {
	sections := make(map[string]any)
	err := toml.Unmarshal(contents, &sections)
	if err != nil {
		return nil, fmt.Errorf("retireDecode: %w", err)
	}
	isChanged := false
	for _, section := range []string{"server", "auth", "content", "storage", "chat", "bootstrap"} {
		_, isPresent := sections[section]
		isChanged = isChanged || isPresent
		delete(sections, section)
	}
	fields, isGameTable := sections["game"].(map[string]any)
	if isGameTable {
		for _, key := range []string{"is_version_locked", "is_singleplayer_only", "campaign_matchmaking_bot_size", "is_campaign_bot_squad_mirroring_enabled"} {
			_, isPresent := fields[key]
			isChanged = isChanged || isPresent
			delete(fields, key)
		}
	}
	if !isChanged {
		return contents, nil
	}
	var replacement bytes.Buffer
	err = toml.NewEncoder(&replacement).Encode(sections)
	if err != nil {
		return nil, fmt.Errorf("retireEncode: %w", err)
	}
	return replacement.Bytes(), nil
}
