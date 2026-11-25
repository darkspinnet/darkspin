package cmd

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	server "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/darkspinnet/darkspin/server/runtime/playerstore"
)

func loadToolConfiguration(path string) (*game.Config, bool, error) {
	if playerstore.IsEnvironmentConfigured() {
		config, isCreated, err := game.LoadDedicatedConfig(path)
		if err != nil {
			return nil, false, fmt.Errorf("dedicatedConfiguration: %w", err)
		}
		return config, isCreated, nil
	}
	config, isCreated, err := server.LoadLocalConfiguration(path)
	if err != nil {
		return nil, false, fmt.Errorf("localConfiguration: %w", err)
	}
	return config, isCreated, nil
}
