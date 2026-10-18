//go:build scenario

package cmd

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/scenario/desktop"
	"github.com/darkspinnet/darkspin/server/scenario/jsonstore"
	"github.com/spf13/cobra"
)

type scenarioRunOptions struct {
	workspace     string
	launcher      string
	gameDirectory string
	content       string
	fang          string
	port          uint16
}

func newScenarioCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "scenario", Short: "Development-only normal-speed scenario capture and comparison",
	}
	command.AddCommand(newScenarioRunCommand())
	command.AddCommand(newScenarioCompareCommand())
	return command
}

func newScenarioRunCommand() *cobra.Command {
	option := &scenarioRunOptions{}
	command := &cobra.Command{
		Use: "run <scenario-file>", Short: "Validate and allocate a fresh isolated 1-2 scenario",
		Args: cobra.ExactArgs(1),
		RunE: option.run,
	}
	command.Flags().StringVar(&option.workspace, "workspace", "", "Dark Spin checkout root (discovered from working directory by default)")
	command.Flags().StringVar(&option.launcher, "launcher", "", "Scenario-built Darkspinner executable")
	command.Flags().StringVar(&option.gameDirectory, "game-directory", "", "Read-only installed game directory")
	command.Flags().StringVar(&option.content, "content", "", "Offline coherent content.db input (no SQLite sidecars)")
	command.Flags().StringVar(&option.fang, "fang", "", "Scenario-built Fang DLL")
	command.Flags().Uint16Var(&option.port, "port", desktop.CanonicalServerPort, "Canonical owned loopback server port; custom ports are unsupported and an occupied service causes failure")
	return command
}

func (e *scenarioRunOptions) run(command *cobra.Command, arguments []string) error {
	err := desktop.ValidatePort(e.port)
	if err != nil {
		return fmt.Errorf("scenarioPort: %w", err)
	}
	inputPath, err := filepath.Abs(arguments[0])
	if err != nil {
		return fmt.Errorf("scenarioInputPath: %w", err)
	}
	req, err := jsonstore.ReadDefinition(command.Context(), inputPath)
	if err != nil {
		return fmt.Errorf("scenarioDefinition: %w", err)
	}
	root, err := jsonstore.WorkspaceRoot(e.workspace)
	if err != nil {
		return fmt.Errorf("scenarioWorkspace: %w", err)
	}
	store, err := jsonstore.New(root)
	if err != nil {
		return fmt.Errorf("scenarioStore: %w", err)
	}
	peerOption, err := e.peerOptions(root)
	if err != nil {
		return fmt.Errorf("scenarioPeerOptions: %w", err)
	}
	peer, err := desktop.New(peerOption)
	if err != nil {
		return fmt.Errorf("scenarioPeer: %w", err)
	}
	runner := scenario.NewRunner(store, peer, peer, peer)
	manifest, runErr := runner.Run(command.Context(), req)
	if manifest.RunID != "" {
		count, outputErr := fmt.Fprintf(command.OutOrStdout(),
			"Run: %s\nOutcome: %s\nManifest: %s\nLast reached: %s\n%s\n",
			manifest.RunID, manifest.Outcome, manifest.Paths.ManifestPath, manifest.LastReached, manifest.Detail)
		if outputErr != nil {
			return fmt.Errorf("scenarioOutput: %w", errors.Join(runErr, outputErr))
		}
		if count == 0 {
			return fmt.Errorf("scenarioEmpty: %w", errors.Join(runErr, errors.New("scenario output was empty")))
		}
	}
	if runErr != nil {
		return fmt.Errorf("scenarioRun: %w", runErr)
	}
	return nil
}

func (e *scenarioRunOptions) peerOptions(root string) (desktop.Options, error) {
	option := desktop.Options{WorkerPath: e.launcher, GameDirectory: e.gameDirectory,
		ContentPath: e.content, FangPath: e.fang, Port: e.port}
	if option.WorkerPath == "" {
		option.WorkerPath = filepath.Join(root, "bin", "game", "darkspinner.exe")
	}
	if option.GameDirectory == "" {
		option.GameDirectory = filepath.Dir(option.WorkerPath)
	}
	if option.ContentPath == "" {
		option.ContentPath = filepath.Join(option.GameDirectory, "darkspin", "cache", "content.db")
	}
	if option.FangPath == "" {
		option.FangPath = filepath.Join(root, "bin", "game", "fang.dll")
	}
	paths := []*string{&option.WorkerPath, &option.GameDirectory, &option.ContentPath, &option.FangPath}
	for _, path := range paths {
		absolute, err := filepath.Abs(*path)
		if err != nil {
			return desktop.Options{}, fmt.Errorf("peerPath: %w", err)
		}
		*path = absolute
	}
	return option, nil
}
