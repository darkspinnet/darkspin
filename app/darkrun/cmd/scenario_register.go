//go:build scenario

package cmd

import "github.com/spf13/cobra"

func registerScenarioCommand(rootCmd *cobra.Command) {
	rootCmd.AddCommand(newScenarioCommand())
}
