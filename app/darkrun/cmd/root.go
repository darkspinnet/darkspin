package cmd

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/darkspinnet/darkspin/server/buildinfo"
	"github.com/spf13/cobra"
)

// Execute runs the command tree until completion or an operating-system signal.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	err := NewRootCommand().ExecuteContext(ctx)
	if err != nil {
		return fmt.Errorf("rootExecute: %w", err)
	}
	return nil
}

// NewRootCommand constructs the root command. Keeping construction free of
// package-level state makes the command tree straightforward to test and embed.
func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "darkrun",
		Short:         "Dark Spin development and server tools",
		Version:       buildinfo.Version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	rootCmd.SetHelpTemplate(fmt.Sprintf("darkrun version %s\n\n%s", buildinfo.Version, rootCmd.HelpTemplate()))

	rootCmd.AddCommand(newServerCommand())
	rootCmd.AddCommand(newAuthCommand())
	rootCmd.AddCommand(newDatabaseCommand())
	rootCmd.AddCommand(newContentCommand())
	rootCmd.AddCommand(newBuildCommand())
	rootCmd.AddCommand(newInspectCommand())
	rootCmd.AddCommand(newLuaCommand())
	rootCmd.AddCommand(newUnzipCommand())
	rootCmd.AddCommand(newZipCommand())
	rootCmd.AddCommand(newInstallDataCommand())
	rootCmd.AddCommand(newMapCommand())
	rootCmd.AddCommand(newSnapshotCommand())
	rootCmd.AddCommand(newConvertCommand())
	rootCmd.AddCommand(newGLTFCommand())
	return rootCmd
}
