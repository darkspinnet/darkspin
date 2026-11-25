package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/spf13/cobra"
)

type serverSetupOptions struct {
	isPrepareOnly  bool
	bindHost       string
	advertisedHost string
	contentPath    string
	port           uint16
}

func (e *serverSetupOptions) registerFlags(command *cobra.Command) {
	command.Flags().BoolVar(&e.isPrepareOnly, "prepare-only", false, "validate environment and prepare private content without starting listeners")
	command.Flags().StringVar(&e.bindHost, "bind-host", "", "override DARKSPIN_BIND_HOST for this server and its children")
	command.Flags().StringVar(&e.advertisedHost, "advertised-host", "", "override DARKSPIN_ADVERTISED_HOST for this server and its children")
	command.Flags().StringVar(&e.contentPath, "content-path", "", "override DARKSPIN_CONTENT_PATH for this server and its children")
	command.Flags().Uint16Var(&e.port, "port", 42127, "override DARKSPIN_PORT for this server and its children")
}

func (e *serverSetupOptions) prepare(command *cobra.Command, configPath string) (*game.Config, error) {
	contentPath := strings.TrimSpace(e.contentPath)
	if command.Flags().Changed("content-path") && contentPath != "" {
		absolutePath, err := filepath.Abs(contentPath)
		if err != nil {
			return nil, fmt.Errorf("contentPath: %w", err)
		}
		contentPath = absolutePath
	}
	settings := []struct {
		flag     string
		variable string
		setting  string
	}{
		{"bind-host", "DARKSPIN_BIND_HOST", strings.TrimSpace(e.bindHost)},
		{"advertised-host", "DARKSPIN_ADVERTISED_HOST", strings.TrimSpace(e.advertisedHost)},
		{"port", "DARKSPIN_PORT", strconv.FormatUint(uint64(e.port), 10)},
		{"content-path", "DARKSPIN_CONTENT_PATH", contentPath},
	}
	for _, setting := range settings {
		if !command.Flags().Changed(setting.flag) {
			continue
		}
		err := os.Setenv(setting.variable, setting.setting)
		if err != nil {
			return nil, fmt.Errorf("environmentSet[%s]: %w", setting.flag, err)
		}
	}
	config, isCreated, err := game.LoadDedicatedConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("settingsLoad: %w", err)
	}
	if isCreated {
		return nil, fmt.Errorf("dedicated settings must come from the environment")
	}
	bindHost, advertisedHost, err := config.NetworkHosts()
	if err != nil {
		return nil, fmt.Errorf("networkSettings: %w", err)
	}
	if bindHost == "" || advertisedHost == "" {
		return nil, fmt.Errorf("server network addresses are required")
	}
	port, err := config.Uint16(game.ConfigServerPort)
	if err != nil {
		return nil, fmt.Errorf("portParse: %w", err)
	}
	if port == 0 || port == ^uint16(0) {
		return nil, fmt.Errorf("server port must be between 1 and 65534")
	}
	return config, nil
}
