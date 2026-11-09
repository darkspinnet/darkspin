package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	contentcache "github.com/darkspinnet/darkspin/content"
	contentruntime "github.com/darkspinnet/darkspin/content/runtime"
	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
	server "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/darkspinnet/darkspin/server/runtime/playerstore"
	"github.com/spf13/cobra"
)

type contentPreflightOptions struct {
	configPath string
	gamePath   string
}

func newContentPreflightCommand() *cobra.Command {
	options := &contentPreflightOptions{}
	command := &cobra.Command{
		Use: "preflight", Short: "Check update content compatibility without changing the running server's files",
		Args: cobra.NoArgs, RunE: options.run,
	}
	configPath := os.Getenv("DARKSPIN_CONFIG_PATH")
	if configPath == "" {
		configPath = game.DefaultConfigFilename
	}
	command.Flags().StringVar(&options.configPath, "config", configPath, "local configuration or dedicated runtime path anchor")
	command.Flags().StringVar(&options.gamePath, "game-path", os.Getenv("DARKSPIN_GAME_PATH"), "required readable build-103 installation for content rebuilds")
	return command
}

func (e *contentPreflightOptions) run(command *cobra.Command, args []string) (resultErr error) {
	err := contentruntime.RequireSource(command.Context(), e.gamePath)
	if err != nil {
		return fmt.Errorf("requiredGameSource: %w", err)
	}
	configPath, err := filepath.Abs(e.configPath)
	if err != nil {
		return fmt.Errorf("configPath: %w", err)
	}
	var config *game.Config
	var isCreated bool
	if playerstore.IsEnvironmentConfigured() {
		config, isCreated, err = game.LoadDedicatedConfig(configPath)
	} else {
		fi, statErr := os.Stat(configPath)
		if statErr != nil {
			return fmt.Errorf("configStat: %w", statErr)
		}
		if !fi.Mode().IsRegular() {
			return errors.New("preflight requires an existing local configuration file")
		}
		config, isCreated, err = server.LoadLocalConfiguration(configPath)
	}
	if err != nil {
		return fmt.Errorf("configLoad: %w", err)
	}
	if isCreated {
		return errors.New("preflight configuration was not available")
	}
	contentPath, err := config.ContentPath(configPath)
	if err != nil {
		return fmt.Errorf("contentPath: %w", err)
	}
	databasePath := filepath.Join(contentPath, server.ContentDatabaseFilename)
	verification, databaseErr := contentsqlite.Verify(command.Context(), databasePath)
	webErr := contentcache.VerifyWeb(command.Context(), contentStaticPath(databasePath))
	if databaseErr == nil && webErr == nil {
		_, err = fmt.Fprintf(command.OutOrStdout(), "Current content is compatible with this image (release %s)\n", verification.ContentRelease)
		if err != nil {
			return fmt.Errorf("preflightOutput: %w", err)
		}
		return nil
	}
	temporaryPath, err := os.MkdirTemp("", "darkspin-content-preflight-")
	if err != nil {
		return fmt.Errorf("temporaryCreate: %w", err)
	}
	defer removePreflightContent(temporaryPath, &resultErr)
	err = ensureServerContent(command.Context(), command.ErrOrStderr(), filepath.Join(temporaryPath, server.ContentDatabaseFilename), e.gamePath)
	if err != nil {
		return fmt.Errorf("temporaryPrepare: %w", err)
	}
	_, err = fmt.Fprintln(command.OutOrStdout(), "Content rebuild succeeded in temporary storage; live content was not changed")
	if err != nil {
		return fmt.Errorf("preparedOutput: %w", err)
	}
	return nil
}

func removePreflightContent(path string, resultErr *error) {
	// The only caller passes the unique directory returned by os.MkdirTemp.
	err := os.RemoveAll(path)
	if err != nil {
		*resultErr = errors.Join(*resultErr, fmt.Errorf("temporaryRemove: %w", err))
	}
}
