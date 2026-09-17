package cmd

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/server/installer"
	"github.com/spf13/cobra"
)

func newInstallDataCommand() *cobra.Command {
	var installPath string
	var version string
	command := &cobra.Command{
		Use:   "install-data",
		Short: "Extract runtime assets from a Game installation",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			workingDirectory, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("workingDir: %w", err)
			}
			if version == "" {
				contents, readErr := os.ReadFile(filepath.Join(installPath, "DarksporeBin", "version_bin.txt"))
				if readErr != nil {
					return fmt.Errorf("versionRead: %w", readErr)
				}
				version = strings.TrimSpace(string(contents))
			}
			dataInstaller := &installer.Installer{}
			err = dataInstaller.Prepare(command.Context(), installer.Options{
				InstallPath: installPath, WorkingDirectory: workingDirectory, Version: version,
				Log: log.New(command.ErrOrStderr(), "", log.LstdFlags).Writer(),
			})
			if err != nil {
				return fmt.Errorf("dataPrepare: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&installPath, "game-path", "../..", "path to the Game installation")
	command.Flags().StringVar(&version, "version", "", "override the detected Game version")
	return command
}
