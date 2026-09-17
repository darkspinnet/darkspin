package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/spf13/cobra"
)

func newInspectCommand() *cobra.Command {
	outputPath := ""
	contains := ""
	command := &cobra.Command{
		Use:   "inspect <package[:entry]>",
		Short: "List or extract a decoded resource from a DBPF package",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			target := parsePackageTarget(args[0])
			r, err := os.Open(target.Path)
			if err != nil {
				return fmt.Errorf("inspectOpen: %w", err)
			}
			defer r.Close()
			fi, err := r.Stat()
			if err != nil {
				return fmt.Errorf("inspectStat: %w", err)
			}
			pkg, err := dbpf.NewReader(r, fi.Size())
			if err != nil {
				return fmt.Errorf("inspectRead: %w", err)
			}
			if target.Selector != "" {
				ordinal, resolveErr := resolvePackageOrdinal(pkg.Entries, target.Selector)
				if resolveErr != nil {
					return fmt.Errorf("inspectSelector: %w", resolveErr)
				}
				if outputPath == "" {
					err = writeInspectedEntry(command.OutOrStdout(), target.Path, pkg, ordinal)
				} else {
					err = writeInspectedResource(command.OutOrStdout(), pkg, ordinal, outputPath)
				}
				if err != nil {
					return fmt.Errorf("inspectResource: %w", err)
				}
				return nil
			}
			if contains != "" {
				err = writeMatchingInspection(command.OutOrStdout(), target.Path, pkg, []byte(contains))
				if err != nil {
					return fmt.Errorf("inspectMatch: %w", err)
				}
				return nil
			}
			err = writeInspection(command.OutOrStdout(), target.Path, pkg)
			if err != nil {
				return fmt.Errorf("inspectOutput: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVarP(&outputPath, "output", "o", "", "optional decoded output path for a selected entry")
	command.Flags().StringVar(&contains, "contains", "", "list decoded resources containing an exact byte string")
	return command
}

func writeInspectedEntry(w io.Writer, sourcePath string, pkg *dbpf.Reader, ordinal int) error {
	if ordinal < 0 || ordinal >= len(pkg.Entries) {
		return fmt.Errorf("ordinalRange: got %d, package has %d resources", ordinal, len(pkg.Entries))
	}
	entry := pkg.Entries[ordinal]
	identity := strings.TrimSuffix(dbpf.ResourceName(ordinal, entry), ".bin")
	_, err := fmt.Fprintf(w, "Package:\t%s\nEntry:\t%s:%s\nOrdinal:\t%d\nType:\t0x%08X\nGroup:\t0x%08X\nInstance:\t0x%016X\nStored:\t%d\nDecoded:\t%d\nCompression:\t%s\n",
		sourcePath, sourcePath, identity, ordinal, entry.Type, entry.Group, entry.Instance,
		entry.StoredSize, entry.Size, compressionName(entry.Compression))
	if err != nil {
		return fmt.Errorf("entryWrite: %w", err)
	}
	return nil
}

func writeMatchingInspection(w io.Writer, sourcePath string, pkg *dbpf.Reader, needle []byte) error {
	_, err := fmt.Fprintf(w, "Package:\t%s\nContains:\t%q\n\n", sourcePath, needle)
	if err != nil {
		return fmt.Errorf("summaryWrite: %w", err)
	}
	for ordinal, entry := range pkg.Entries {
		r, openErr := pkg.Open(entry)
		if openErr != nil {
			return fmt.Errorf("resourceOpen[%d]: %w", ordinal, openErr)
		}
		contents, readErr := io.ReadAll(r)
		if readErr != nil {
			return fmt.Errorf("resourceRead[%d]: %w", ordinal, readErr)
		}
		if !bytes.Contains(contents, needle) {
			continue
		}
		_, err = fmt.Fprintf(w, "%d\t%s\t0x%08X\t0x%08X\t0x%016X\t%d\n",
			ordinal, dbpf.ResourceName(ordinal, entry), entry.Type, entry.Group, entry.Instance, len(contents))
		if err != nil {
			return fmt.Errorf("matchWrite[%d]: %w", ordinal, err)
		}
	}
	return nil
}

func writeInspectedResource(output io.Writer, pkg *dbpf.Reader, ordinal int, outputPath string) error {
	if ordinal < 0 || ordinal >= len(pkg.Entries) {
		return fmt.Errorf("ordinalRange: got %d, package has %d resources", ordinal, len(pkg.Entries))
	}
	if outputPath == "" {
		return errors.New("outputMissing: use --output with --ordinal")
	}
	r, err := pkg.Open(pkg.Entries[ordinal])
	if err != nil {
		return fmt.Errorf("resourceOpen[%d]: %w", ordinal, err)
	}
	contents, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("resourceRead[%d]: %w", ordinal, err)
	}
	err = os.WriteFile(outputPath, contents, 0o600)
	if err != nil {
		return fmt.Errorf("resourceWrite[%d]: %w", ordinal, err)
	}
	_, err = fmt.Fprintf(output, "Decoded resource %d to %s (%d bytes)\n", ordinal, outputPath, len(contents))
	if err != nil {
		return fmt.Errorf("resourceOutput: %w", err)
	}
	return nil
}

func writeInspection(w io.Writer, sourcePath string, pkg *dbpf.Reader) error {
	_, err := fmt.Fprintf(w, "Package:\t%s\nDBPF:\t%d.%d\nIndex:\t%d\nResources:\t%d\n\n",
		sourcePath, pkg.Header.MajorVersion, pkg.Header.MinorVersion, pkg.Header.IndexVersion, len(pkg.Entries))
	if err != nil {
		return fmt.Errorf("summaryWrite: %w", err)
	}
	table := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, err = fmt.Fprintln(table, "ORDINAL\tRESOURCE\tTYPE\tGROUP\tINSTANCE\tSTORED\tDECODED\tCOMPRESSION")
	if err != nil {
		return fmt.Errorf("headerWrite: %w", err)
	}
	for ordinal, entry := range pkg.Entries {
		_, err = fmt.Fprintf(table, "%d\t%s\t0x%08X\t0x%08X\t0x%016X\t%d\t%d\t%s\n",
			ordinal, dbpf.ResourceName(ordinal, entry), entry.Type, entry.Group, entry.Instance,
			entry.StoredSize, entry.Size, compressionName(entry.Compression))
		if err != nil {
			return fmt.Errorf("entryWrite[%d]: %w", ordinal, err)
		}
	}
	err = table.Flush()
	if err != nil {
		return fmt.Errorf("tableFlush: %w", err)
	}
	return nil
}

func compressionName(compression uint16) string {
	switch compression {
	case 0:
		return "none"
	case 0xffff:
		return "refpack"
	default:
		return fmt.Sprintf("0x%04X", compression)
	}
}

func newUnzipCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "unzip <package> [destination]",
		Short: "Extract a DBPF package",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(command *cobra.Command, args []string) error {
			explicitPath := ""
			if len(args) == 2 {
				explicitPath = args[1]
			}
			destinationPath := unzipDestination(args[0], explicitPath)
			err := dbpf.ExtractPath(command.Context(), args[0], destinationPath)
			if err != nil {
				return fmt.Errorf("unzipExtract: %w", err)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Extracted %s to %s\n", args[0], destinationPath)
			if err != nil {
				return fmt.Errorf("unzipOutput: %w", err)
			}
			return nil
		},
	}
}

func newZipCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "zip <directory> [destination]",
		Short: "Build a DBPF package from an extracted directory",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(command *cobra.Command, args []string) error {
			explicitPath := ""
			if len(args) == 2 {
				explicitPath = args[1]
			}
			destinationPath, err := zipDestination(args[0], explicitPath)
			if err != nil {
				return fmt.Errorf("zipDestination: %w", err)
			}
			err = dbpf.PackPath(command.Context(), args[0], destinationPath)
			if err != nil {
				return fmt.Errorf("zipPack: %w", err)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Packed %s to %s\n", args[0], destinationPath)
			if err != nil {
				return fmt.Errorf("zipOutput: %w", err)
			}
			return nil
		},
	}
}

func unzipDestination(sourcePath, explicitPath string) string {
	if explicitPath != "" {
		return explicitPath
	}
	return filepath.Join(filepath.Dir(sourcePath), "_"+filepath.Base(sourcePath))
}

func zipDestination(sourcePath, explicitPath string) (string, error) {
	if explicitPath != "" {
		return explicitPath, nil
	}
	cleanPath := filepath.Clean(sourcePath)
	sourceName := filepath.Base(cleanPath)
	if !strings.HasPrefix(sourceName, "_") {
		return "", fmt.Errorf("%q has no underscore prefix", sourceName)
	}
	packageName := strings.TrimPrefix(sourceName, "_")
	if packageName == "" {
		return "", errors.New("underscore prefix has no package name")
	}
	return filepath.Join(filepath.Dir(cleanPath), packageName), nil
}
