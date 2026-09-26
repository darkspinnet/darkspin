package snapshot

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// InspectRequest selects one local snapshot bundle and whether its derived
// timeline and reports should be deterministically regenerated.
type InspectRequest struct {
	Directory string
	IsRewrite bool
}

// BundleFileInspection describes one manifest-owned artifact and its current
// integrity result.
type BundleFileInspection struct {
	Name              string
	Size              int64
	SHA256            string
	ExpectedSize      int64
	ExpectedSHA256    string
	IsDigestAvailable bool
	IsMatch           bool
	IsSource          bool
}

// BundleInspection is the transport-neutral result of offline validation and
// analysis regeneration.
type BundleInspection struct {
	Directory            string
	SnapshotID           string
	FormatVersion        uint32
	LikelyCause          string
	Confidence           string
	FindingCount         int
	ReplayEventCount     int
	ClientEventCount     int
	IsIntegrityAvailable bool
	IsIntegrityValid     bool
	IsRewritten          bool
	Files                []BundleFileInspection
}

// InspectBundle validates one immutable capture and regenerates its derived
// replay and analysis in memory. Rewrite mode replaces only derived artifacts
// and the manifest; original transport and state evidence is never changed.
func InspectBundle(ctx context.Context, req InspectRequest) (BundleInspection, error) {
	err := ctx.Err()
	if err != nil {
		return BundleInspection{}, fmt.Errorf("inspectContext: %w", err)
	}
	directory, err := requireRegularDirectory(req.Directory)
	if err != nil {
		return BundleInspection{}, fmt.Errorf("inspectDirectory: %w", err)
	}
	metadata := manifest{}
	err = readJSONFile(filepath.Join(directory, "manifest.json"), &metadata)
	if err != nil {
		return BundleInspection{}, fmt.Errorf("inspectManifest: %w", err)
	}
	if strings.TrimSpace(metadata.ID) == "" {
		return BundleInspection{}, errors.New("snapshot manifest ID missing")
	}
	files, isIntegrityAvailable, isIntegrityValid, isSourceIntegrityValid, err :=
		inspectBundleFiles(directory, metadata.Files)
	if err != nil {
		return BundleInspection{}, fmt.Errorf("inspectFiles: %w", err)
	}
	events, err := readTrafficEvents(filepath.Join(directory, "raknet.jsonl"))
	if err != nil {
		return BundleInspection{}, fmt.Errorf("inspectTraffic: %w", err)
	}
	state := StateFrame{}
	err = readJSONFile(filepath.Join(directory, "server-state.json"), &state)
	if err != nil {
		return BundleInspection{}, fmt.Errorf("inspectState: %w", err)
	}
	clientFile := "client.jsonl"
	if metadata.IsClientMemoryCaptured || metadata.IsClientMemoryAvailable {
		clientFile = "client-memory.jsonl"
	}
	clientLines, err := readSnapshotLines(filepath.Join(directory, clientFile))
	if err != nil {
		return BundleInspection{}, fmt.Errorf("inspectClient: %w", err)
	}
	boundaryAt := metadata.CreatedAt
	if metadata.ClientBoundaryAt != nil {
		boundaryAt = *metadata.ClientBoundaryAt
	}
	requestName := metadata.ID + "_" + metadata.CreatedAt.UTC().Format("20060102T150405.000000000Z")
	timeline := buildReplayTimeline(
		events, clientLines, clientFile, requestName, boundaryAt,
	)
	capture := clientCapture{
		IsCaptured: metadata.IsClientMemoryCaptured, Status: metadata.ClientCaptureStatus,
		RequestedAt: boundaryAt, ClientReceivedTimeMS: metadata.ClientReceivedTimeMS,
		KeyframeStartedTimeMS:   metadata.ClientKeyframeStartedTimeMS,
		KeyframeCompletedTimeMS: metadata.ClientKeyframeCompletedTimeMS,
	}
	if metadata.ClientResponseAt != nil {
		capture.RespondedAt = *metadata.ClientResponseAt
	}
	analysisRequest := dumpRequest{
		Actor: metadata.Actor, Trigger: metadata.Trigger, Context: metadata.Context,
		Fingerprint: metadata.Fingerprint, ObjectID: metadata.ObjectID,
	}
	if metadata.TriggerObservedAt != nil {
		analysisRequest.TriggeredAt = *metadata.TriggerObservedAt
	} else {
		for _, incident := range metadata.AutomaticIncidents {
			if incident.Fingerprint == metadata.Fingerprint {
				analysisRequest.TriggeredAt = incident.TriggeredAt
				break
			}
		}
	}
	for _, artifact := range metadata.Files {
		switch artifact.Name {
		case "server-state-trigger.json":
			triggerState := StateFrame{}
			err = readJSONFile(filepath.Join(directory, artifact.Name), &triggerState)
			if err != nil {
				return BundleInspection{}, fmt.Errorf("inspectTrigger: %w", err)
			}
			analysisRequest.TriggerState = &triggerState
		case "movement.json":
			err = readJSONFile(filepath.Join(directory, artifact.Name), &analysisRequest.Movements)
			if err != nil {
				return BundleInspection{}, fmt.Errorf("inspectMovement: %w", err)
			}
		}
	}
	analysis := analyzeSnapshot(
		metadata.ID, analysisRequest,
		state, events, timeline, capture, metadata.DroppedEventCount, metadata.CreatedAt,
	)
	analysis.TransportDiagnostics = metadata.TransportDiagnostics
	analysis.TriggerTransportDiagnostics = metadata.TriggerTransportDiagnostics
	inspection := BundleInspection{
		Directory: directory, SnapshotID: metadata.ID,
		FormatVersion: metadata.FormatVersion, LikelyCause: analysis.LikelyCause,
		Confidence: analysis.Confidence, FindingCount: len(analysis.Findings),
		ReplayEventCount: len(timeline.Events), ClientEventCount: timeline.ClientEventCount,
		IsIntegrityAvailable: isIntegrityAvailable,
		IsIntegrityValid:     isIntegrityValid,
		Files:                files,
	}
	if !req.IsRewrite {
		return inspection, nil
	}
	if isIntegrityAvailable && !isSourceIntegrityValid {
		return inspection, errors.New("source artifact integrity failed; refusing to rewrite derived files")
	}
	err = validateRewriteTargets(directory)
	if err != nil {
		return inspection, fmt.Errorf("inspectTargets: %w", err)
	}
	err = rewriteDerivedFiles(directory, &metadata, timeline, analysis)
	if err != nil {
		return inspection, fmt.Errorf("inspectRewrite: %w", err)
	}
	files, isIntegrityAvailable, isIntegrityValid, isSourceIntegrityValid, err =
		inspectBundleFiles(directory, metadata.Files)
	if err != nil {
		return inspection, fmt.Errorf("inspectRecheck: %w", err)
	}
	if !isSourceIntegrityValid {
		isIntegrityValid = false
	}
	inspection.FormatVersion = metadata.FormatVersion
	inspection.IsIntegrityAvailable = isIntegrityAvailable
	inspection.IsIntegrityValid = isIntegrityValid
	inspection.IsRewritten = true
	inspection.Files = files
	return inspection, nil
}

func validateRewriteTargets(directory string) error {
	for _, name := range []string{
		"timeline.jsonl", "analysis.json", "report.md", "manifest.json",
	} {
		path := filepath.Join(directory, name)
		fi, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("targetStat[%s]: %w", name, err)
		}
		if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
			return fmt.Errorf("targetType[%s]: derived target is not a regular file", name)
		}
	}
	return nil
}

func rewriteDerivedFiles(
	directory string, metadata *manifest, timeline replayTimeline, analysis analysisReport,
) error {
	err := writeReplayTimeline(filepath.Join(directory, "timeline.jsonl"), timeline.Events)
	if err != nil {
		return fmt.Errorf("rewriteTimeline: %w", err)
	}
	err = writeJSON(filepath.Join(directory, "analysis.json"), analysis)
	if err != nil {
		return fmt.Errorf("rewriteAnalysis: %w", err)
	}
	err = writeAnalysisMarkdown(filepath.Join(directory, "report.md"), analysis)
	if err != nil {
		return fmt.Errorf("rewriteReport: %w", err)
	}
	metadata.FormatVersion = 16
	metadata.ReplayEventCount = len(timeline.Events)
	metadata.ClientReplayEventCount = timeline.ClientEventCount
	metadata.ClientMalformedCount = timeline.ClientMalformedLineCount
	metadata.IsClientTimelineAligned = timeline.IsClientAligned
	metadata.ClientRingLineCount = timeline.ClientRingLineCount
	metadata.ClientRingByteCount = timeline.ClientRingByteCount
	metadata.ClientCapacityDroppedLineCount = timeline.ClientCapacityDroppedLineCount
	metadata.ClientCapacityDroppedByteCount = timeline.ClientCapacityDroppedByteCount
	metadata.IsClientRingTruncated = timeline.IsClientRingTruncated
	metadata.FindingCount = len(analysis.Findings)
	metadata.LikelyCause = analysis.LikelyCause
	metadata.Confidence = analysis.Confidence
	metadata.Files = ensureDerivedFileInfos(metadata.Files)
	metadata.Files, err = hashFileInfos(directory, metadata.Files)
	if err != nil {
		return fmt.Errorf("rewriteHash: %w", err)
	}
	err = writeJSON(filepath.Join(directory, "manifest.json"), metadata)
	if err != nil {
		return fmt.Errorf("rewriteManifest: %w", err)
	}
	return nil
}

func ensureDerivedFileInfos(files []fileInfo) []fileInfo {
	for _, name := range []string{"timeline.jsonl", "analysis.json", "report.md"} {
		isFound := false
		for _, current := range files {
			if current.Name == name {
				isFound = true
				break
			}
		}
		if isFound {
			continue
		}
		files = append(files, fileInfo{Name: name, Description: derivedFileDescription(name)})
	}
	return files
}

func derivedFileDescription(name string) string {
	switch name {
	case "timeline.jsonl":
		return "Clock-aligned replay index joining server transport, application delivery, client frame/action state, and RakNet-associated client object state"
	case "analysis.json":
		return "Machine-readable transport, pending-output, action/input, lifecycle, delivery, projectile-position, and client locomotion-stall comparisons"
	case "report.md":
		return "Readable root-cause summary with cited source lines and next checks"
	default:
		return "Derived snapshot artifact"
	}
}

func inspectBundleFiles(
	directory string, files []fileInfo,
) ([]BundleFileInspection, bool, bool, bool, error) {
	inspections := make([]BundleFileInspection, 0, len(files))
	isIntegrityAvailable := len(files) > 0
	isIntegrityValid := true
	isSourceIntegrityValid := true
	for index, current := range files {
		if !isBundleFileName(current.Name) {
			return nil, false, false, false,
				fmt.Errorf("fileName[%d]: invalid %q", index, current.Name)
		}
		size, digest, err := hashFile(filepath.Join(directory, current.Name))
		if errors.Is(err, os.ErrNotExist) {
			isIntegrityValid = false
			if isSourceBundleFile(current.Name) {
				isSourceIntegrityValid = false
			}
			inspections = append(inspections, BundleFileInspection{
				Name: current.Name, ExpectedSize: current.Size,
				ExpectedSHA256:    current.SHA256,
				IsDigestAvailable: hasExpectedDigest(current),
				IsSource:          isSourceBundleFile(current.Name),
			})
			continue
		}
		if err != nil {
			return nil, false, false, false,
				fmt.Errorf("fileHash[%s]: %w", current.Name, err)
		}
		isDigestAvailable := hasExpectedDigest(current)
		isMatch := isDigestAvailable && isDigestMatch(current, size, digest)
		if !isDigestAvailable {
			isIntegrityAvailable = false
		} else if !isMatch {
			isIntegrityValid = false
			if isSourceBundleFile(current.Name) {
				isSourceIntegrityValid = false
			}
		}
		inspections = append(inspections, BundleFileInspection{
			Name: current.Name, Size: size, SHA256: digest,
			ExpectedSize: current.Size, ExpectedSHA256: current.SHA256,
			IsDigestAvailable: isDigestAvailable, IsMatch: isMatch,
			IsSource: isSourceBundleFile(current.Name),
		})
	}
	if !isIntegrityAvailable {
		isIntegrityValid = false
	}
	return inspections, isIntegrityAvailable, isIntegrityValid, isSourceIntegrityValid, nil
}

func readTrafficEvents(path string) ([]trafficEvent, error) {
	r, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("trafficOpen: %w", err)
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	events := make([]trafficEvent, 0, 1024)
	line := 0
	for scanner.Scan() {
		line++
		event := trafficEvent{}
		err = json.Unmarshal(scanner.Bytes(), &event)
		if err != nil {
			closeErr := r.Close()
			if closeErr != nil {
				return nil, fmt.Errorf("trafficDecode[%d]: %w; close: %v", line, err, closeErr)
			}
			return nil, fmt.Errorf("trafficDecode[%d]: %w", line, err)
		}
		events = append(events, event)
	}
	err = scanner.Err()
	closeErr := r.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		if closeErr != nil {
			return nil, fmt.Errorf("trafficScan: %w; close: %v", err, closeErr)
		}
		return nil, fmt.Errorf("trafficScan: %w", err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("trafficClose: %w", closeErr)
	}
	return events, nil
}

func readJSONFile(path string, document any) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("jsonRead: %w", err)
	}
	err = json.Unmarshal(contents, document)
	if err != nil {
		return fmt.Errorf("jsonDecode: %w", err)
	}
	return nil
}
