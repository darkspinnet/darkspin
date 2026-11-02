package cmd

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	contentruntime "github.com/darkspinnet/darkspin/content/runtime"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/instance"
	server "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/darkspinnet/darkspin/server/runtime/playerstore"
	"github.com/spf13/cobra"
)

func newServerCommand() *cobra.Command {
	options := &serverCommandOptions{}

	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Start the combined account login and game server (PostgreSQL required)",
		Args:  cobra.NoArgs,
		RunE:  options.run,
	}
	options.setup.registerFlags(serverCmd)

	serverCmd.Flags().StringVar(
		&options.contentGamePath,
		"content-game-path",
		"",
		"required content source installation; defaults to --game-path or DarksporeBin beside the executable",
	)
	serverCmd.Flags().StringVar(
		&options.server.GamePath,
		"game-path",
		"",
		"required readable build-103 installation retained for content rebuilds; defaults beside the executable",
	)
	serverCmd.Flags().StringVar(
		&options.server.ConfigPath,
		"config",
		game.DefaultConfigFilename,
		"path to the server configuration",
	)
	serverCmd.Flags().BoolVarP(
		&options.server.IsTimestampingEnabled,
		"timestamps",
		"t",
		false,
		"include timestamps in server logs",
	)
	serverCmd.Flags().StringVar(
		&options.server.BlazeCertificatePath,
		"blaze-cert",
		"",
		"path to a PEM TLS certificate for Blaze listeners",
	)
	serverCmd.Flags().StringVar(
		&options.server.BlazePrivateKeyPath,
		"blaze-key",
		"",
		"path to the PEM private key for --blaze-cert",
	)
	serverCmd.Flags().StringVar(
		&options.server.TracePath,
		"trace",
		"",
		"write redacted protocol events as JSONL",
	)

	return serverCmd
}

type serverCommandOptions struct {
	server          server.Options
	contentGamePath string
	setup           serverSetupOptions
}

func (e *serverCommandOptions) run(cmd *cobra.Command, _ []string) (resultErr error) {
	err := applyServerEnvironment(cmd)
	if err != nil {
		return fmt.Errorf("serverEnvironment: %w", err)
	}
	e.server.Postgres, err = playerstore.FromEnvironment()
	if err != nil {
		return fmt.Errorf("postgresEnvironment: %w", err)
	}
	contentGamePath := e.contentGamePath
	if contentGamePath == "" {
		contentGamePath = e.server.GamePath
	}
	gamePath, err := resolveContentGamePath(contentGamePath)
	if err != nil {
		return fmt.Errorf("contentGamePath: %w", err)
	}
	configPath, err := filepath.Abs(e.server.ConfigPath)
	if err != nil {
		return fmt.Errorf("configPath: %w", err)
	}
	e.server.ConfigPath = configPath
	bootstrapRoot := os.Getenv("DARKSPIN_BOOTSTRAP_ROOT")
	if bootstrapRoot != "" {
		bootstrapRoot, err = filepath.Abs(bootstrapRoot)
		if err != nil {
			return fmt.Errorf("bootstrapRoot: %w", err)
		}
		err = validateBootstrapPaths(cmd, bootstrapRoot, configPath, e.setup.contentPath)
		if err != nil {
			return fmt.Errorf("bootstrapLayout: %w", err)
		}
	}
	e.server.RuntimePath = filepath.Join(filepath.Dir(configPath), server.DarkspinDirectory)
	instanceLease, err := instance.Acquire(e.server.RuntimePath)
	if errors.Is(err, instance.ErrRunning) {
		if e.setup.isPrepareOnly {
			return fmt.Errorf("prepareBusy: %w", err)
		}
		_, outputErr := fmt.Fprintf(cmd.ErrOrStderr(),
			"A Dark Spin server is already running or starting in %s; leaving it running without changing its settings.\n",
			e.server.RuntimePath)
		if outputErr != nil {
			return fmt.Errorf("instanceNotice: %w", outputErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("instanceAcquire: %w", err)
	}
	defer func() {
		closeErr := instanceLease.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("instanceClose: %w", closeErr))
		}
	}()
	e.server.InstanceLease = instanceLease
	err = contentruntime.RequireSource(cmd.Context(), gamePath)
	if err != nil {
		return fmt.Errorf("requiredGameSource: %w", err)
	}
	e.server.GamePath = gamePath
	config, err := e.setup.prepare(cmd, configPath)
	if err != nil {
		return fmt.Errorf("serverSetup: %w", err)
	}
	if !config.Bool(game.ConfigServerIsDedicated) {
		return errors.New("PostgreSQL player storage requires --dedicated")
	}
	if bootstrapRoot != "" {
		e.server.MaintenanceSchedule, err = bootstrapSchedule(config, bootstrapRoot)
		if err != nil {
			return fmt.Errorf("bootstrapSchedule: %w", err)
		}
	}
	contentPath, err := config.ContentPath(configPath)
	if err != nil {
		return fmt.Errorf("contentPath: %w", err)
	}
	databasePath := filepath.Join(contentPath, server.ContentDatabaseFilename)
	err = ensureServerContent(cmd.Context(), cmd.ErrOrStderr(), databasePath, gamePath)
	if err != nil {
		return fmt.Errorf("contentEnsure: %w", err)
	}
	flags := 0
	if e.server.IsTimestampingEnabled {
		flags = log.LstdFlags | log.Lmicroseconds
	}
	e.server.Logger = log.New(cmd.ErrOrStderr(), "", flags)
	if e.setup.isPrepareOnly {
		e.server.Logger.Printf("Server settings and private content prepared; config=%q content=%q", configPath, contentPath)
		return nil
	}
	app, err := server.New(e.server)
	if err != nil {
		return fmt.Errorf("serverInit: %w", err)
	}
	err = app.Run(cmd.Context())
	if err != nil {
		return fmt.Errorf("serverRun: %w", err)
	}
	return nil
}
