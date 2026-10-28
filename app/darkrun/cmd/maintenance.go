package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/maintenance"
	server "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/spf13/cobra"
)

type maintenanceOptions struct {
	configPath string
	minutes    int
	id         string
	isReady    bool
}

func newMaintenanceCommand() *cobra.Command {
	options := &maintenanceOptions{}
	command := &cobra.Command{Use: "maintenance", Short: "Request, announce, or cancel maintenance through the running server's local control channel"}
	configPath := os.Getenv("DARKSPIN_CONFIG_PATH")
	if configPath == "" {
		configPath = game.DefaultConfigFilename
	}
	command.PersistentFlags().StringVar(&options.configPath, "config", configPath, "path to the running server's internal configuration")
	announce := &cobra.Command{Use: "announce", Short: "Warn online players before a server update", Args: cobra.NoArgs, RunE: options.run}
	announce.Flags().IntVar(&options.minutes, "minutes", 30, "minutes before restart (1 to 1440)")
	update := &cobra.Command{Use: "update", Short: "Warn players, stop cleanly, and ask bootstrap to back up and self-update", Args: cobra.NoArgs, RunE: options.run}
	update.Flags().IntVar(&options.minutes, "minutes", 30, "warning interval before update (0 to 1440; 0 updates immediately)")
	cancel := &cobra.Command{Use: "cancel", Short: "Cancel a previously announced update", Args: cobra.NoArgs, RunE: options.run}
	cancel.Flags().StringVar(&options.id, "id", "", "only cancel the matching countdown ID")
	status := &cobra.Command{Use: "status", Short: "Read local server liveness and the current countdown", Args: cobra.NoArgs, RunE: options.run}
	status.Flags().BoolVar(&options.isReady, "ready", false, "also verify the main server endpoint and image identity")
	command.AddCommand(announce, update, cancel, status)
	return command
}

func (e *maintenanceOptions) run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if e.isReady {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	configPath, err := filepath.Abs(e.configPath)
	if err != nil {
		return fmt.Errorf("configPath: %w", err)
	}
	runtimePath := filepath.Join(filepath.Dir(configPath), server.DarkspinDirectory)
	reply, err := maintenance.Execute(ctx, runtimePath, cmd.Name(), e.minutes, e.id)
	if err != nil {
		return fmt.Errorf("maintenanceRequest: %w", err)
	}
	if e.isReady {
		err = checkMaintenanceReadiness(ctx, configPath)
		if err != nil {
			return fmt.Errorf("serverReadiness: %w", err)
		}
	}
	err = json.NewEncoder(cmd.OutOrStdout()).Encode(reply)
	if err != nil {
		return fmt.Errorf("maintenanceOutput: %w", err)
	}
	return nil
}
