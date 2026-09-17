package cmd

import (
	"fmt"
	"path/filepath"

	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	server "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/spf13/cobra"
)

func newBuildCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "build",
		Short: "Build offline Game data products",
	}
	command.AddCommand(newMetaBuildCommand())
	return command
}

func newMetaBuildCommand() *cobra.Command {
	var gamePath string
	var databasePath string
	command := &cobra.Command{
		Use:   "meta",
		Short: "Build meta.db from a vanilla Game installation",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			resolvedGamePath, err := resolveContentGamePath(gamePath)
			if err != nil {
				return fmt.Errorf("gamePath: %w", err)
			}
			err = contentsqlite.BuildMeta(command.Context(), contentsqlite.BuildOptions{
				GamePath:     resolvedGamePath,
				DatabasePath: databasePath,
			})
			if err != nil {
				return fmt.Errorf("metaBuild: %w", err)
			}
			verification, err := contentsqlite.VerifyMeta(command.Context(), databasePath)
			if err != nil {
				return fmt.Errorf("metaVerify: %w", err)
			}
			_, err = fmt.Fprintf(
				command.OutOrStdout(),
				"Built %s from %s (%d Lua resources, %d compiled animations)\n",
				databasePath,
				resolvedGamePath,
				verification.CompiledLuaCount,
				verification.CompiledAnimationCount,
			)
			if err != nil {
				return fmt.Errorf("buildOutput: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&gamePath, "game-path", "", "path to DarksporeBin or its installation root; defaults beside the executable")
	command.Flags().StringVar(
		&databasePath,
		"output",
		filepath.Join(server.DarkspinDirectory, server.CacheDirectory, contentsqlite.MetaFilename),
		"output metadata database",
	)
	return command
}
