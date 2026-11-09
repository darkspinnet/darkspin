package game

import (
	"bytes"
	"fmt"

	"github.com/BurntSushi/toml"
)

// RetireMatchmakingConfiguration removes legacy settings only after their
// replacement database row has been successfully initialized.
func (e *Config) RetireMatchmakingConfiguration(path string) error {
	if e.Bool(ConfigServerIsDedicated) {
		return nil
	}
	configurationMutex.Lock()
	defer configurationMutex.Unlock()
	previousContents, err := readConfiguration(path)
	if err != nil {
		return fmt.Errorf("legacyRead: %w", err)
	}
	sections := make(map[string]any)
	err = toml.Unmarshal(previousContents, &sections)
	if err != nil {
		return fmt.Errorf("legacyDecode: %w", err)
	}
	fields, isTable := sections["game"].(map[string]any)
	if !isTable {
		return nil
	}
	_, isSizePresent := fields["campaign_matchmaking_bot_size"]
	_, isMirroringPresent := fields["is_campaign_bot_squad_mirroring_enabled"]
	if !isSizePresent && !isMirroringPresent {
		return nil
	}
	delete(fields, "campaign_matchmaking_bot_size")
	delete(fields, "is_campaign_bot_squad_mirroring_enabled")
	var contents bytes.Buffer
	err = toml.NewEncoder(&contents).Encode(sections)
	if err != nil {
		return fmt.Errorf("legacyEncode: %w", err)
	}
	err = replaceConfiguration(path, previousContents, contents.Bytes())
	if err != nil {
		return fmt.Errorf("legacyWrite: %w", err)
	}
	return nil
}
