//go:build scenario

package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario/jsonstore"
	"github.com/darkspinnet/darkspin/server/snapshot"
	"github.com/spf13/cobra"
)

const maximumScenarioCaptureSize = 32 << 20

type scenarioCompareOptions struct {
	workspace         string
	reportDirectory   string
	positionTolerance float64
	rotationTolerance float64
	scaleTolerance    float64
	maximumInterval   time.Duration
}

func newScenarioCompareCommand() *cobra.Command {
	options := &scenarioCompareOptions{}
	command := &cobra.Command{
		Use:   "compare <capture-file>",
		Short: "Compare immutable normal-speed scenario evidence offline",
		Args:  cobra.ExactArgs(1),
		RunE:  options.run,
	}
	command.Flags().StringVar(&options.workspace, "workspace", "", "Dark Spin checkout root (discovered from working directory by default)")
	command.Flags().StringVar(&options.reportDirectory, "report-directory", "", "new report directory within this run's scenario logs")
	command.Flags().Float64Var(&options.positionTolerance, "position-tolerance", 0.05, "position tolerance in world units")
	command.Flags().Float64Var(&options.rotationTolerance, "rotation-tolerance", 0.01, "rotation tolerance in captured component units")
	command.Flags().Float64Var(&options.scaleTolerance, "scale-tolerance", 0.001, "scale tolerance")
	command.Flags().DurationVar(&options.maximumInterval, "maximum-interval", 250*time.Millisecond, "maximum correlated capture window")
	return command
}

func (e *scenarioCompareOptions) run(command *cobra.Command, arguments []string) error {
	err := command.Context().Err()
	if err != nil {
		return fmt.Errorf("compareContext: %w", err)
	}
	capture, source, err := readScenarioCapture(arguments[0])
	if err != nil {
		return fmt.Errorf("compareRead: %w", err)
	}
	if !isScenarioRunID(capture.RunID) {
		return errors.New("capture run ID must be a simple identifier")
	}
	comparison, err := snapshot.CompareScenario(command.Context(), snapshot.ScenarioCompareRequest{
		Capture: capture, PositionTolerance: e.positionTolerance,
		RotationTolerance: e.rotationTolerance, ScaleTolerance: e.scaleTolerance,
		MaximumInterval: e.maximumInterval,
	})
	if err != nil {
		return fmt.Errorf("compareAnalyze: %w", err)
	}
	comparison.SourceCapture = source
	directory := e.reportDirectory
	if directory == "" {
		root, rootErr := jsonstore.WorkspaceRoot(e.workspace)
		if rootErr != nil {
			return fmt.Errorf("compareWorkspace: %w", rootErr)
		}
		directory = filepath.Join(root, "bin", "game", "logs", "scenarios", capture.RunID)
	}
	err = command.Context().Err()
	if err != nil {
		return fmt.Errorf("compareDeadline: %w", err)
	}
	paths, err := snapshot.WriteScenarioReport(command.Context(), snapshot.ScenarioReportRequest{
		Directory: directory, Comparison: comparison,
	})
	if err != nil {
		return fmt.Errorf("compareReport: %w", err)
	}
	writtenSize, err := fmt.Fprintf(command.OutOrStdout(),
		"Scenario %s: %s\nReport: %s\nResult: %s\n",
		capture.RunID, comparison.Outcome, paths.MarkdownPath, paths.JSONPath)
	if err != nil {
		return fmt.Errorf("comparePrint: %w", err)
	}
	if writtenSize == 0 {
		return errors.New("scenario comparison output was not written")
	}
	if comparison.Outcome != "passed" {
		return fmt.Errorf("scenario comparison %s; inspect %s", comparison.Outcome, paths.MarkdownPath)
	}
	return nil
}

func readScenarioCapture(path string) (snapshot.ScenarioCapture, snapshot.ScenarioEvidence, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return snapshot.ScenarioCapture{}, snapshot.ScenarioEvidence{}, fmt.Errorf("capturePath: %w", err)
	}
	r, err := os.Open(absolutePath)
	if err != nil {
		return snapshot.ScenarioCapture{}, snapshot.ScenarioEvidence{}, fmt.Errorf("captureOpen: %w", err)
	}
	fi, statErr := r.Stat()
	if statErr != nil {
		closeErr := r.Close()
		return snapshot.ScenarioCapture{}, snapshot.ScenarioEvidence{}, fmt.Errorf("captureStat: %w", errors.Join(statErr, closeErr))
	}
	if !fi.Mode().IsRegular() {
		closeErr := r.Close()
		return snapshot.ScenarioCapture{}, snapshot.ScenarioEvidence{}, fmt.Errorf("captureType: %w", errors.Join(errors.New("scenario capture must be a regular file"), closeErr))
	}
	payload, readErr := io.ReadAll(io.LimitReader(r, maximumScenarioCaptureSize+1))
	closeErr := r.Close()
	if readErr != nil {
		return snapshot.ScenarioCapture{}, snapshot.ScenarioEvidence{}, fmt.Errorf("captureRead: %w", errors.Join(readErr, closeErr))
	}
	if closeErr != nil {
		return snapshot.ScenarioCapture{}, snapshot.ScenarioEvidence{}, fmt.Errorf("captureClose: %w", closeErr)
	}
	if len(payload) > maximumScenarioCaptureSize {
		return snapshot.ScenarioCapture{}, snapshot.ScenarioEvidence{}, errors.New("scenario capture exceeds 32 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	capture := snapshot.ScenarioCapture{}
	err = decoder.Decode(&capture)
	if err != nil {
		return snapshot.ScenarioCapture{}, snapshot.ScenarioEvidence{}, fmt.Errorf("captureDecode: %w", err)
	}
	var trailing any
	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		if err != nil {
			return snapshot.ScenarioCapture{}, snapshot.ScenarioEvidence{}, fmt.Errorf("captureTrailing: %w", err)
		}
		return snapshot.ScenarioCapture{}, snapshot.ScenarioEvidence{}, errors.New("scenario capture contains trailing JSON")
	}
	digest := sha256.Sum256(payload)
	size := int64(len(payload))
	source := snapshot.ScenarioEvidence{Path: absolutePath, Size: &size, SHA256: hex.EncodeToString(digest[:])}
	return capture, source, nil
}

func isScenarioRunID(runID string) bool {
	if len(runID) == 0 || len(runID) > 80 {
		return false
	}
	for _, character := range runID {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}
