//go:build scenario

package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ScenarioReportRequest struct {
	Directory  string
	Comparison ScenarioComparison
}

type ScenarioReportPaths struct {
	JSONPath     string
	MarkdownPath string
}

// WriteScenarioReport exclusively creates derived files beneath the named run's
// bin/game/logs/scenarios directory. Existing files, including captures, cannot
// be replaced. Symlink, junction and other irregular ancestors are rejected.
// If the second write fails, the completed JSON is retained and its path returned.
func WriteScenarioReport(ctx context.Context, req ScenarioReportRequest) (ScenarioReportPaths, error) {
	err := ctx.Err()
	if err != nil {
		return ScenarioReportPaths{}, fmt.Errorf("reportContext: %w", err)
	}
	directory, err := scenarioReportDirectory(req.Directory, req.Comparison.RunID)
	if err != nil {
		return ScenarioReportPaths{}, fmt.Errorf("reportDirectory: %w", err)
	}
	paths := ScenarioReportPaths{
		JSONPath:     filepath.Join(directory, "scenario-comparison.json"),
		MarkdownPath: filepath.Join(directory, "scenario-comparison.md"),
	}
	evidences := scenarioReportEvidence(req.Comparison)
	for _, evidence := range evidences {
		if evidence.Path == "" {
			continue
		}
		sourcePath, pathErr := filepath.Abs(evidence.Path)
		if pathErr != nil {
			return ScenarioReportPaths{}, fmt.Errorf("sourceAbs: %w", pathErr)
		}
		if strings.EqualFold(sourcePath, paths.JSONPath) || strings.EqualFold(sourcePath, paths.MarkdownPath) {
			return ScenarioReportPaths{}, errors.New("scenario report path references source capture evidence")
		}
	}
	payload, err := json.MarshalIndent(req.Comparison, "", "  ")
	if err != nil {
		return ScenarioReportPaths{}, fmt.Errorf("reportMarshal: %w", err)
	}
	payload = append(payload, '\n')
	document := scenarioReportMarkdown(req.Comparison)
	err = ctx.Err()
	if err != nil {
		return ScenarioReportPaths{}, fmt.Errorf("reportPrepared: %w", err)
	}
	err = os.MkdirAll(directory, 0o700)
	if err != nil {
		return ScenarioReportPaths{}, fmt.Errorf("reportMkdir: %w", err)
	}
	// Recheck after creating parents so none of the created path is assumed safe.
	err = scenarioReportAncestors(directory)
	if err != nil {
		return ScenarioReportPaths{}, fmt.Errorf("reportAncestors: %w", err)
	}
	err = scenarioWriteExclusive(paths.JSONPath, payload)
	if err != nil {
		return ScenarioReportPaths{}, fmt.Errorf("reportJSON: %w", err)
	}
	err = ctx.Err()
	if err != nil {
		paths.MarkdownPath = ""
		return paths, fmt.Errorf("reportInterrupted: %w", err)
	}
	err = scenarioWriteExclusive(paths.MarkdownPath, []byte(document))
	if err != nil {
		paths.MarkdownPath = ""
		return paths, fmt.Errorf("reportMarkdown: %w", err)
	}
	return paths, nil
}

func scenarioReportDirectory(directory, runID string) (string, error) {
	if directory == "" || !scenarioIsSafeRunID(runID) {
		return "", errors.New("scenario report directory or safe run ID missing")
	}
	absolutePath, err := filepath.Abs(directory)
	if err != nil {
		return "", fmt.Errorf("reportAbs: %w", err)
	}
	// Find the exact fixed run directory in the ancestor chain, then allow only
	// that directory or its descendants. Do not accept substring prefix matches.
	current := absolutePath
	isContained := false
	for {
		if strings.EqualFold(filepath.Base(current), runID) {
			parent := filepath.Dir(current)
			for _, segment := range []string{"scenarios", "logs", "game", "bin"} {
				if !strings.EqualFold(filepath.Base(parent), segment) {
					break
				}
				parent = filepath.Dir(parent)
				if segment == "bin" {
					isContained = true
				}
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	if !isContained {
		return "", errors.New("scenario reports must stay under bin/game/logs/scenarios/<run-id>")
	}
	err = scenarioReportAncestors(absolutePath)
	if err != nil {
		return "", fmt.Errorf("reportPath: %w", err)
	}
	return absolutePath, nil
}

func scenarioIsSafeRunID(runID string) bool {
	if runID == "" || runID == "." || runID == ".." || strings.TrimSpace(runID) != runID || strings.HasSuffix(runID, ".") {
		return false
	}
	for _, character := range runID {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func scenarioReportAncestors(path string) error {
	current := path
	for {
		fi, err := os.Lstat(current)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("ancestorStat: %w", err)
		}
		if err == nil && (fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || !fi.IsDir()) {
			return errors.New("scenario report ancestor must be a regular directory without symlink or junction")
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func scenarioWriteExclusive(path string, payload []byte) error {
	w, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("outputOpen: %w", err)
	}
	writtenCount, writeErr := w.Write(payload)
	closeErr := w.Close()
	if writeErr != nil {
		if closeErr != nil {
			return fmt.Errorf("outputWrite: %w; close: %v", writeErr, closeErr)
		}
		return fmt.Errorf("outputWrite: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("outputClose: %w", closeErr)
	}
	if writtenCount != len(payload) {
		return errors.New("scenario report write was incomplete")
	}
	return nil
}

func scenarioReportEvidence(comparison ScenarioComparison) []ScenarioEvidence {
	evidences := []ScenarioEvidence{comparison.SourceCapture}
	evidences = append(evidences, comparison.RawEvidence...)
	for _, parity := range []ScenarioParity{comparison.LayoutParity, comparison.ReplicationParity} {
		for _, finding := range parity.Findings {
			evidences = append(evidences, finding.Evidence...)
			for _, object := range []*ScenarioObject{finding.Expected, finding.Observed} {
				if object != nil {
					evidences = append(evidences, object.Evidence...)
				}
			}
		}
	}
	return scenarioUniqueEvidence(evidences)
}

func scenarioReportMarkdown(comparison ScenarioComparison) string {
	paragraphs := []string{}
	paragraphs = append(paragraphs, fmt.Sprintf("# Scenario comparison: %s\n\nRun: %s. Outcome: **%s**.\n\n",
		scenarioMarkdownText(comparison.ScenarioID), scenarioMarkdownText(comparison.RunID), scenarioMarkdownText(comparison.Outcome)))
	paragraphs = append(paragraphs, fmt.Sprintf("Position tolerance: %.6g world units. Rotation tolerance: %.6g captured component units. Scale tolerance: %.6g. Maximum correlated interval: %.6g ms. These intervals do not establish atomic sampling.\n\n",
		comparison.Tolerances.Position, comparison.Tolerances.Rotation, comparison.Tolerances.Scale, comparison.Tolerances.MaximumIntervalMS))
	if comparison.SourceCapture.Path != "" {
		paragraphs = append(paragraphs, fmt.Sprintf("Source capture: %s. SHA256: `%s`.\n\n", scenarioEvidenceLink(comparison.SourceCapture), scenarioMarkdownText(comparison.SourceCapture.SHA256)))
	}
	for index, parity := range []ScenarioParity{comparison.LayoutParity, comparison.ReplicationParity} {
		name := "Layout parity"
		if index == 1 {
			name = "Replication parity"
		}
		paragraphs = append(paragraphs, fmt.Sprintf("## %s\n\n**%s**; %d boundaries, %d expected records, %d observed records, %d confirmed matches.\n\n",
			name, scenarioMarkdownText(parity.Outcome), parity.BoundaryCount, parity.ExpectedObjectCount, parity.ObservedObjectCount, parity.MatchedObjectCount))
		if len(parity.Findings) == 0 {
			paragraphs = append(paragraphs, "No discrepancy findings; absent comparable evidence remains inconclusive.\n\n")
			continue
		}
		findings := scenarioHumanFindings(parity.Findings)
		for _, finding := range findings {
			paragraphs = append(paragraphs, fmt.Sprintf("- **%s / %s** at %s (%s): %s",
				scenarioMarkdownText(finding.Outcome), scenarioMarkdownText(finding.Kind), scenarioMarkdownText(finding.Boundary),
				scenarioMarkdownText(finding.Confidence), scenarioMarkdownText(finding.Detail)))
			if finding.Identity != "" {
				paragraphs = append(paragraphs, fmt.Sprintf(" Identity: %s.", scenarioMarkdownText(finding.Identity)))
			}
			for _, evidence := range finding.Evidence {
				paragraphs = append(paragraphs, " "+scenarioEvidenceLink(evidence))
			}
			paragraphs = append(paragraphs, "\n")
		}
		if len(findings) < len(parity.Findings) {
			paragraphs = append(paragraphs, fmt.Sprintf("- %d additional findings are retained in [the machine report](scenario-comparison.json).\n", len(parity.Findings)-len(findings)))
		}
		paragraphs = append(paragraphs, "\n")
	}
	paragraphs = append(paragraphs, "## Earliest supported divergence\n\n")
	if comparison.EarliestDivergence == nil || comparison.EarliestDivergence.OccurredAt == nil {
		paragraphs = append(paragraphs, "Unavailable: no discrepant observation has both a supported wall-clock time and raw evidence.\n\n")
	} else {
		finding := comparison.EarliestDivergence
		paragraphs = append(paragraphs, fmt.Sprintf("%s / %s at %s. %s\n\n", scenarioMarkdownText(finding.Boundary), scenarioMarkdownText(finding.Kind),
			finding.OccurredAt.UTC().Format("2006-01-02T15:04:05.999999999Z"), scenarioMarkdownText(finding.Detail)))
		for _, evidence := range finding.Evidence {
			paragraphs = append(paragraphs, "- "+scenarioEvidenceLink(evidence)+"\n")
		}
		paragraphs = append(paragraphs, "\n")
	}
	paragraphs = append(paragraphs, "## Policy limitations\n\n")
	for _, limitation := range comparison.PolicyLimitations {
		paragraphs = append(paragraphs, "- "+scenarioMarkdownText(limitation)+"\n")
	}
	return strings.Join(paragraphs, "")
}

func scenarioHumanFindings(findings []ScenarioFinding) []ScenarioFinding {
	selecteds := make([]ScenarioFinding, 0, 16)
	for _, outcome := range []string{"failed", "inconclusive"} {
		for _, finding := range findings {
			if finding.Outcome == outcome {
				selecteds = append(selecteds, finding)
			}
			if len(selecteds) == 16 {
				return selecteds
			}
		}
	}
	return selecteds
}

func scenarioMarkdownText(text string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "\r", " ", "\n", " ")
	return replacer.Replace(text)
}

func scenarioEvidenceLink(evidence ScenarioEvidence) string {
	label := scenarioMarkdownText(filepath.Base(evidence.Path))
	path := filepath.ToSlash(evidence.Path)
	path = strings.NewReplacer("<", "%3C", ">", "%3E", "\r", "%0D", "\n", "%0A").Replace(path)
	if evidence.Line > 0 {
		label += fmt.Sprintf(":%d", evidence.Line)
		path += fmt.Sprintf(":%d", evidence.Line)
	}
	return fmt.Sprintf("[%s](<%s>)", label, path)
}
