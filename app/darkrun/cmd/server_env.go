package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// applyServerEnvironment uses the same parsing and persistence path as explicit
// flags. Empty environment settings leave the saved configuration untouched.
func applyServerEnvironment(command *cobra.Command) error {
	settings := []struct {
		flag     string
		variable string
	}{
		{"advertised-host", "DARKSPIN_ADVERTISED_HOST"},
		{"game-path", "DARKSPIN_GAME_PATH"},
		{"content-path", "DARKSPIN_CONTENT_PATH"},
		{"config", "DARKSPIN_CONFIG_PATH"},
		{"bind-host", "DARKSPIN_BIND_HOST"},
		{"port", "DARKSPIN_PORT"},
		{"dedicated", "DARKSPIN_IS_DEDICATED"},
	}
	for _, setting := range settings {
		if command.Flags().Changed(setting.flag) {
			continue
		}
		argument, isPresent := os.LookupEnv(setting.variable)
		if !isPresent || strings.TrimSpace(argument) == "" {
			continue
		}
		err := command.Flags().Set(setting.flag, strings.TrimSpace(argument))
		if err != nil {
			return fmt.Errorf("environmentSet[%s]: %w", setting.variable, err)
		}
	}
	return nil
}
