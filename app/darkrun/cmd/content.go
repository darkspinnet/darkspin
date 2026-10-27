package cmd

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	contentcache "github.com/darkspinnet/darkspin/content"
	contentruntime "github.com/darkspinnet/darkspin/content/runtime"
	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	server "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/spf13/cobra"
)

func newContentCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "content",
		Short: "Build and verify immutable server content",
	}
	command.AddCommand(newContentBuildCommand())
	command.AddCommand(newContentVerifyCommand())
	command.AddCommand(newContentPreflightCommand())
	return command
}

func newContentBuildCommand() *cobra.Command {
	var gamePath string
	var databasePath string
	command := &cobra.Command{
		Use:   "build",
		Short: "Prepare content.db and adjacent web assets from an installed Game client",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			resolvedGamePath, err := resolveContentGamePath(gamePath)
			if err != nil {
				return fmt.Errorf("gamePath: %w", err)
			}
			err = contentruntime.Ensure(command.Context(), contentruntime.Options{
				GamePath:     resolvedGamePath,
				DatabasePath: databasePath,
			})
			if err != nil {
				return fmt.Errorf("contentBuild: %w", err)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Prepared %s and adjacent web assets\n", databasePath)
			if err != nil {
				return fmt.Errorf("buildOutput: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&gamePath, "game-path", "", "path to DarksporeBin or its installation root; defaults beside the executable")
	command.Flags().StringVar(&databasePath, "output", filepath.Join(server.DarkspinDirectory, server.CacheDirectory, server.ContentDatabaseFilename), "output content database")
	return command
}

func contentStaticPath(databasePath string) string {
	return filepath.Join(filepath.Dir(databasePath), "www", "static")
}

func resolveContentGamePath(explicitPath string) (string, error) {
	if explicitPath != "" {
		return explicitPath, nil
	}
	executablePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("executablePath: %w", err)
	}
	return contentGamePathForExecutable(executablePath), nil
}

func contentGamePathForExecutable(executablePath string) string {
	return filepath.Join(filepath.Dir(executablePath), "DarksporeBin")
}

func newContentVerifyCommand() *cobra.Command {
	var databasePath string
	command := &cobra.Command{
		Use:   "verify",
		Short: "Verify content.db and its adjacent prepared web assets",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			verification, err := contentsqlite.Verify(command.Context(), databasePath)
			if err != nil {
				return fmt.Errorf("contentVerify: %w", err)
			}
			err = contentcache.VerifyWeb(command.Context(), contentStaticPath(databasePath))
			if err != nil {
				return fmt.Errorf("webVerify: %w", err)
			}
			_, err = fmt.Fprintf(
				command.OutOrStdout(),
				"Verified %s and adjacent web assets (build %d, release %s)\n",
				databasePath,
				verification.SourceBuild,
				verification.ContentRelease,
			)
			if err != nil {
				return fmt.Errorf("verifyOutput: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&databasePath, "database", filepath.Join(server.DarkspinDirectory, server.CacheDirectory, server.ContentDatabaseFilename), "content database to verify")
	return command
}

func ensureServerContent(ctx context.Context, output io.Writer, databasePath, gamePath string) error {
	if output == nil {
		output = io.Discard
	}
	reporter := contentPreparationLog{logger: log.New(output, "", 0)}
	reporter.logger.Printf("Checking prepared content in %s", filepath.Dir(databasePath))
	err := contentruntime.Ensure(ctx, contentruntime.Options{
		GamePath: gamePath, DatabasePath: databasePath,
		OnProgress: reporter.progress, OnStageTiming: reporter.stageTiming,
	})
	if err != nil {
		return fmt.Errorf("contentPrepare: %w", err)
	}
	return nil
}

type contentPreparationLog struct {
	logger *log.Logger
}

func (e contentPreparationLog) progress(progress contentsqlite.BuildProgress) {
	e.logger.Printf("Content: %s (%d/%d)", progress.Phase, progress.Completed, progress.Total)
}

func (e contentPreparationLog) stageTiming(timing contentsqlite.BuildStageTiming) {
	e.logger.Printf("Content import phase=%q elapsed=%s complete=%t", timing.Phase, timing.Duration, timing.IsComplete)
}
