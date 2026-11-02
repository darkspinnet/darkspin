package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/spf13/cobra"
)

type serverSetupOptions struct {
	isDedicated    bool
	isPrepareOnly  bool
	bindHost       string
	advertisedHost string
	contentPath    string
	port           uint16
}

func (e *serverSetupOptions) registerFlags(command *cobra.Command) {
	command.Flags().BoolVar(&e.isDedicated, "dedicated", false, "save password-required login mode (enabled for new configurations)")
	command.Flags().BoolVar(&e.isPrepareOnly, "prepare-only", false, "prepare settings and private content without starting listeners")
	command.Flags().StringVar(&e.bindHost, "bind-host", "", "save the IPv4 listener address (empty selects automatic binding)")
	command.Flags().StringVar(&e.advertisedHost, "advertised-host", "", "save the IPv4 address clients use to reach this server")
	command.Flags().StringVar(&e.contentPath, "content-path", "", "save the prepared content directory containing content.db and www/static")
	command.Flags().Uint16Var(&e.port, "port", 42127, "save the server TCP/UDP port")
}

func (e *serverSetupOptions) prepare(command *cobra.Command, configPath string) (*game.Config, error) {
	fi, err := os.Stat(configPath)
	isCreated := errors.Is(err, os.ErrNotExist)
	if err != nil && !isCreated {
		return nil, fmt.Errorf("settingsStat: %w", err)
	}
	config := game.DefaultConfig()
	if !isCreated {
		if fi.IsDir() {
			return nil, errors.New("config path must name a file")
		}
		config, isCreated, err = game.LoadConfig(configPath)
		if err != nil {
			return nil, fmt.Errorf("settingsLoad: %w", err)
		}
	}
	if isCreated && !command.Flags().Changed("dedicated") {
		err = config.Set(game.ConfigServerIsDedicated, "true")
		if err != nil {
			return nil, fmt.Errorf("dedicatedDefault: %w", err)
		}
	}
	settings := []struct {
		flag    string
		key     game.ConfigKey
		setting string
	}{
		{"dedicated", game.ConfigServerIsDedicated, strconv.FormatBool(e.isDedicated)},
		{"bind-host", game.ConfigServerBindHost, strings.TrimSpace(e.bindHost)},
		{"advertised-host", game.ConfigServerAdvertisedHost, strings.TrimSpace(e.advertisedHost)},
		{"port", game.ConfigServerPort, strconv.FormatUint(uint64(e.port), 10)},
	}
	isChanged := isCreated
	for _, setting := range settings {
		if !command.Flags().Changed(setting.flag) {
			continue
		}
		err = config.Set(setting.key, setting.setting)
		if err != nil {
			return nil, fmt.Errorf("settingSet[%s]: %w", setting.flag, err)
		}
		isChanged = true
	}
	if command.Flags().Changed("content-path") {
		contentPath := strings.TrimSpace(e.contentPath)
		if contentPath != "" {
			contentPath, err = filepath.Abs(contentPath)
			if err != nil {
				return nil, fmt.Errorf("contentPath: %w", err)
			}
		}
		err = config.Set(game.ConfigContentPath, contentPath)
		if err != nil {
			return nil, fmt.Errorf("contentSet: %w", err)
		}
		isChanged = true
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
	isSecretChanged, err := config.PrepareDedicatedAuth()
	if err != nil {
		return nil, fmt.Errorf("authPrepare: %w", err)
	}
	if isChanged || isSecretChanged {
		err = config.Save(configPath)
		if err != nil {
			return nil, fmt.Errorf("settingsSave: %w", err)
		}
	}
	return config, nil
}
