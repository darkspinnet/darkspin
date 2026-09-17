package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	contentcache "github.com/darkspinnet/darkspin/content"
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
	return command
}

func newContentBuildCommand() *cobra.Command {
	var gamePath string
	var databasePath string
	command := &cobra.Command{
		Use:   "build",
		Short: "Build content.db from an installed Game client",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			resolvedGamePath, err := resolveContentGamePath(gamePath)
			if err != nil {
				return fmt.Errorf("gamePath: %w", err)
			}
			err = contentcache.PrepareWeb(command.Context(), contentcache.WebOptions{
				GamePath: resolvedGamePath, StaticPath: contentStaticPath(databasePath),
			})
			if err != nil {
				return fmt.Errorf("webPrepare: %w", err)
			}
			err = contentsqlite.Build(command.Context(), contentsqlite.BuildOptions{
				GamePath:     resolvedGamePath,
				DatabasePath: databasePath,
			})
			if err != nil {
				return fmt.Errorf("contentBuild: %w", err)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Initialized %s from %s\n", databasePath, resolvedGamePath)
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
		Short: "Verify an existing content.db",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			verification, err := contentsqlite.Verify(command.Context(), databasePath)
			if err != nil {
				return fmt.Errorf("contentVerify: %w", err)
			}
			_, err = fmt.Fprintf(
				command.OutOrStdout(),
				"Verified %s (build %d, release %s)\n",
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

func ensureContentDatabase(ctx context.Context, output io.Writer, databasePath, gamePath string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if output == nil {
		output = io.Discard
	}
	_, err := os.Stat(databasePath)
	if err == nil {
		_, err = contentsqlite.Verify(ctx, databasePath)
		if err != nil {
			return fmt.Errorf("contentVerify: %w", err)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("contentStat: %w", err)
	}
	_, err = fmt.Fprintf(output, "content.db is missing; building from %s\n", gamePath)
	if err != nil {
		return fmt.Errorf("buildNotice: %w", err)
	}
	err = contentsqlite.Build(ctx, contentsqlite.BuildOptions{
		GamePath:     gamePath,
		DatabasePath: databasePath,
	})
	if err != nil {
		return fmt.Errorf("contentBuild: %w", err)
	}
	_, err = contentsqlite.Verify(ctx, databasePath)
	if err != nil {
		return fmt.Errorf("contentVerifyBuilt: %w", err)
	}
	return nil
}
