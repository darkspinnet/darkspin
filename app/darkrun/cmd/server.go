package cmd

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	contentcache "github.com/darkspinnet/darkspin/content"
	"github.com/darkspinnet/darkspin/server/game"
	server "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/spf13/cobra"
)

func newServerCommand() *cobra.Command {
	options := &serverCommandOptions{}

	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Start the Game server",
		Args:  cobra.NoArgs,
		RunE:  options.run,
	}

	serverCmd.Flags().StringVar(
		&options.contentGamePath,
		"content-game-path",
		"",
		"path to DarksporeBin used when content.db must be built; defaults beside the executable",
	)
	serverCmd.Flags().StringVar(
		&options.server.GamePath,
		"game-path",
		"../..",
		"path to the Game installation",
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
}

func (e *serverCommandOptions) run(cmd *cobra.Command, _ []string) error {
	gamePath, err := resolveContentGamePath(e.contentGamePath)
	if err != nil {
		return fmt.Errorf("contentGamePath: %w", err)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("workingDir: %w", err)
	}
	databasePath := filepath.Join(
		workingDirectory, server.DarkspinDirectory, server.CacheDirectory,
		server.ContentDatabaseFilename,
	)
	err = contentcache.PrepareWeb(cmd.Context(), contentcache.WebOptions{
		GamePath: gamePath, StaticPath: contentStaticPath(databasePath),
	})
	if err != nil {
		return fmt.Errorf("webPrepare: %w", err)
	}
	err = ensureContentDatabase(cmd.Context(), cmd.ErrOrStderr(), databasePath, gamePath)
	if err != nil {
		return fmt.Errorf("contentEnsure: %w", err)
	}
	flags := 0
	if e.server.IsTimestampingEnabled {
		flags = log.LstdFlags | log.Lmicroseconds
	}
	e.server.Logger = log.New(cmd.ErrOrStderr(), "", flags)
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
