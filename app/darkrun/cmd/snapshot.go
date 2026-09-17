package cmd

import (
	"fmt"
	"io"

	"github.com/darkspinnet/darkspin/server/snapshot"
	"github.com/spf13/cobra"
)

func newSnapshotCommand() *cobra.Command {
	isRewrite := false
	command := &cobra.Command{
		Use:   "snapshot <bundle-directory>",
		Short: "Verify and reanalyze a Sync Snapshot bundle",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			inspection, err := snapshot.InspectBundle(command.Context(), snapshot.InspectRequest{
				Directory: arguments[0], IsRewrite: isRewrite,
			})
			if err != nil {
				return fmt.Errorf("snapshotInspect: %w", err)
			}
			err = writeSnapshotInspection(command.OutOrStdout(), inspection)
			if err != nil {
				return fmt.Errorf("snapshotOutput: %w", err)
			}
			return nil
		},
	}
	command.Flags().BoolVar(
		&isRewrite, "rewrite", false,
		"replace derived timeline and analysis files after source integrity validation",
	)
	return command
}

func writeSnapshotInspection(output io.Writer, inspection snapshot.BundleInspection) error {
	integrity := "unavailable"
	if inspection.IsIntegrityAvailable && inspection.IsIntegrityValid {
		integrity = "valid"
	} else if inspection.IsIntegrityAvailable {
		integrity = "INVALID"
	}
	_, err := fmt.Fprintf(
		output,
		"Snapshot:\t%s\nDirectory:\t%s\nFormat:\t%d\nIntegrity:\t%s\nLikely cause:\t%s (%s confidence)\nFindings:\t%d\nReplay events:\t%d\nClient events:\t%d\nRewritten:\t%t\n\n",
		inspection.SnapshotID, inspection.Directory, inspection.FormatVersion,
		integrity, inspection.LikelyCause, inspection.Confidence,
		inspection.FindingCount, inspection.ReplayEventCount,
		inspection.ClientEventCount, inspection.IsRewritten,
	)
	if err != nil {
		return fmt.Errorf("snapshotSummary: %w", err)
	}
	for index, current := range inspection.Files {
		role := "derived"
		if current.IsSource {
			role = "source"
		}
		status := "digest unavailable"
		if current.IsDigestAvailable && current.IsMatch {
			status = "verified"
		} else if current.IsDigestAvailable && current.SHA256 == "" {
			status = "missing"
		} else if current.IsDigestAvailable {
			status = "MISMATCH"
		}
		_, err = fmt.Fprintf(
			output, "%s\t%s\t%d bytes\t%s\n",
			current.Name, role, current.Size, status,
		)
		if err != nil {
			return fmt.Errorf("snapshotFile[%d]: %w", index, err)
		}
	}
	return nil
}
