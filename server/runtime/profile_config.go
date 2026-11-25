package runtime

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/runtime/playerstore"
)

// Administrative CLI operations explicitly opt into the dedicated deployment's
// PostgreSQL environment. Desktop/scenario startup never calls this selector.
func loadAdministrativeConfiguration(configPath string) (*game.Config, error) {
	var config *game.Config
	var isGenerated bool
	var err error
	if playerstore.IsEnvironmentConfigured() {
		config, isGenerated, err = game.LoadDedicatedConfig(configPath)
	} else {
		config, isGenerated, err = LoadLocalConfiguration(configPath)
	}
	if err != nil {
		return nil, fmt.Errorf("adminConfiguration: %w", err)
	}
	if isGenerated && config.Bool(game.ConfigServerIsDedicated) {
		return nil, fmt.Errorf("dedicated configuration cannot be generated")
	}
	return config, nil
}
