//go:build scenario

package snapshot

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type scenarioRecorderState struct {
	isScenarioConfigured   bool
	scenarioRunID          string
	scenarioTraceDirectory string
}

type ScenarioRecorderRequest struct {
	RunID           string
	ReportDirectory string
	TraceDirectory  string
}

type ScenarioDumpRequest struct {
	UserID   int64
	Boundary string
}

type ScenarioDump struct {
	Request        string
	Directory      string
	StartedAt      time.Time
	CompletedAt    time.Time
	Evidences      []ScenarioEvidence
	ClientEvidence ScenarioEvidence
	ClientLines    []string
	State          StateFrame
	StateEvidence  ScenarioEvidence
}

// ConfigureScenario relocates the existing recorder before its owned server
// starts. Client memory follows the control file's parent in Fang; protocol
// recording remains independently configured in the runtime composition.
func (e *Service) ConfigureScenario(ctx context.Context, req ScenarioRecorderRequest) error {
	if ctx == nil || e == nil {
		return errors.New("scenario recorder requires context and service")
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("recorderContext: %w", err)
	}
	directory, err := scenarioReportDirectory(req.ReportDirectory, req.RunID)
	if err != nil {
		return fmt.Errorf("recorderDirectory: %w", err)
	}
	if filepath.Base(directory) != req.RunID {
		return errors.New("scenario recorder requires the exact run report root")
	}
	traceDirectory, err := scenarioTraceDirectory(req.TraceDirectory, req.RunID)
	if err != nil {
		return fmt.Errorf("recorderTrace: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.controlMu.Lock()
	defer e.controlMu.Unlock()
	if e.isScenarioConfigured || e.mode != ModeOff || len(e.events) != 0 ||
		e.nextEventSequence != 0 || e.nextIncident.Load() != 0 {
		return errors.New("scenario recorder must be configured once before collection")
	}
	select {
	case <-e.close:
		return errors.New("scenario recorder is closed")
	default:
	}
	snapshotDirectory := filepath.Join(directory, "snapshots")
	err = os.Mkdir(snapshotDirectory, 0700)
	if err != nil {
		return fmt.Errorf("recorderAllocate: %w", err)
	}
	err = scenarioReportAncestors(snapshotDirectory)
	if err != nil {
		return fmt.Errorf("recorderAncestors: %w", err)
	}
	controlPath := filepath.Join(directory, "snapshot-control.txt")
	err = scenarioWriteExclusive(controlPath, []byte("mode=manual\n"))
	if err != nil {
		return fmt.Errorf("recorderControl: %w", err)
	}
	e.directory = snapshotDirectory
	e.archiveDirectory = ""
	e.traceDirectory = directory
	e.controlPath = controlPath
	e.mode = ModeManual
	e.scenarioRunID = req.RunID
	e.scenarioTraceDirectory = traceDirectory
	e.isScenarioConfigured = true
	return nil
}

// DumpScenario requests the existing bounded snapshot bundle. A successfully
// written dump is diagnostic evidence, not an observed gameplay milestone.
func (e *Service) DumpScenario(ctx context.Context, req ScenarioDumpRequest) (ScenarioDump, error) {
	result := ScenarioDump{StartedAt: time.Now().UTC()}
	if ctx == nil || e == nil || req.UserID <= 0 {
		return result, errors.New("scenario dump requires context, service and isolated user")
	}
	switch req.Boundary {
	case "native_layout", "terminal_failure", "fixture_comparison":
	default:
		return result, errors.New("unsupported scenario diagnostic boundary")
	}
	e.mu.Lock()
	isConfigured := e.isScenarioConfigured && e.mode == ModeManual
	directory := e.directory
	runID := e.scenarioRunID
	e.mu.Unlock()
	if !isConfigured {
		return result, errors.New("scenario recorder is not configured for manual collection")
	}
	err := scenarioReportAncestors(directory)
	if err != nil {
		return result, fmt.Errorf("dumpAncestors: %w", err)
	}
	bundle, err := e.writeScenarioDump(ctx, req)
	result = bundle
	if err != nil {
		return result, fmt.Errorf("scenarioDump: %w", err)
	}
	err = scenarioReportAncestors(bundle.Directory)
	if err != nil {
		return result, fmt.Errorf("bundleAncestors: %w", err)
	}
	validatedDirectory, err := scenarioReportDirectory(bundle.Directory, runID)
	if err != nil || filepath.Dir(validatedDirectory) != directory {
		if err != nil {
			return result, fmt.Errorf("bundleDirectory: %w", err)
		}
		return result, errors.New("scenario dump escaped its allocated snapshot directory")
	}
	entries, err := os.ReadDir(bundle.Directory)
	if err != nil {
		return result, fmt.Errorf("bundleList: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(bundle.Directory, entry.Name())
		isRetained := false
		for _, retained := range result.Evidences {
			if retained.Path == path {
				isRetained = true
				break
			}
		}
		if isRetained {
			continue
		}
		if entry.Name() == "client-memory.jsonl" {
			// A request timeout can leave a file that Fang is still writing.
			// Only the validated completed response below enters raw evidence.
			continue
		}
		evidence, hashErr := scenarioDumpEvidence(ctx, path)
		if hashErr != nil {
			return result, fmt.Errorf("bundleHash[%s]: %w", entry.Name(), hashErr)
		}
		result.Evidences = append(result.Evidences, evidence)
		if entry.Name() == "client-memory.jsonl" {
			result.ClientEvidence = evidence
		}
		if entry.Name() == "server-state.json" {
			result.StateEvidence = evidence
		}
	}
	return result, nil
}

func scenarioTraceDirectory(path, runID string) (string, error) {
	if path == "" || !scenarioIsSafeRunID(runID) {
		return "", errors.New("scenario protocol directory or run ID missing")
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("traceAbs: %w", err)
	}
	current := absolutePath
	for _, segment := range []string{runID, "traces", "logs", "darkspin", "server", "bin"} {
		if !strings.EqualFold(filepath.Base(current), segment) {
			return "", errors.New("scenario protocol captures must stay at bin/server/darkspin/logs/traces/<run-id>")
		}
		current = filepath.Dir(current)
	}
	err = scenarioReportAncestors(absolutePath)
	if err != nil {
		return "", fmt.Errorf("traceAncestors: %w", err)
	}
	return absolutePath, nil
}

// Reuses the existing recorder's ring and capture primitives. The protocol
// copy has its own owned trace directory; client/state evidence stays with
// the run's report. This is a dump adapter, not another observer or recorder.
func (e *Service) writeScenarioDump(ctx context.Context, req ScenarioDumpRequest) (ScenarioDump, error) {
	result := ScenarioDump{StartedAt: time.Now().UTC()}
	e.mu.Lock()
	e.pruneLocked(result.StartedAt)
	events := cloneTrafficEvents(e.events)
	droppedCount := uint64(len(e.droppedEventTimes))
	bufferDuration := e.bufferDuration
	traceDirectory := e.scenarioTraceDirectory
	clientDirectory := e.traceDirectory
	result.Request = fmt.Sprintf("SS-%06d_%s", e.nextIncident.Add(1), result.StartedAt.Format("20060102T150405.000000000Z"))
	result.Directory = filepath.Join(e.directory, result.Request)
	e.mu.Unlock()
	err := ctx.Err()
	if err != nil {
		return result, fmt.Errorf("captureContext: %w", err)
	}
	err = os.Mkdir(result.Directory, 0700)
	if err != nil {
		return result, fmt.Errorf("captureAllocate: %w", err)
	}
	protocolRoot := filepath.Join(traceDirectory, "snapshots")
	err = os.MkdirAll(protocolRoot, 0700)
	if err != nil {
		return result, fmt.Errorf("protocolRoot: %w", err)
	}
	err = scenarioReportAncestors(protocolRoot)
	if err != nil {
		return result, fmt.Errorf("protocolAncestors: %w", err)
	}
	protocolDirectory := filepath.Join(protocolRoot, result.Request)
	err = os.Mkdir(protocolDirectory, 0700)
	if err != nil {
		return result, fmt.Errorf("protocolAllocate: %w", err)
	}
	protocolPath := filepath.Join(protocolDirectory, "raknet.jsonl")
	byteCount, err := writeTraffic(protocolPath, events)
	if err != nil {
		return result, fmt.Errorf("protocolWrite: %w", err)
	}
	protocolEvidence, err := scenarioDumpEvidence(ctx, protocolPath)
	if err != nil {
		return result, fmt.Errorf("protocolHash: %w", err)
	}
	result.Evidences = append(result.Evidences, protocolEvidence)
	state, stateStatus := e.captureState(ctx, Actor{UserID: req.UserID})
	result.State = state
	err = writeJSON(filepath.Join(result.Directory, "server-state.json"), state)
	if err != nil {
		return result, fmt.Errorf("captureState: %w", err)
	}
	result.StateEvidence, err = scenarioDumpEvidence(ctx, filepath.Join(result.Directory, "server-state.json"))
	if err != nil {
		return result, fmt.Errorf("captureStateHash: %w", err)
	}
	result.Evidences = append(result.Evidences, result.StateEvidence)
	clientStatus := e.requestClientMemory(ctx, result.Request,
		filepath.Join(result.Directory, "client-memory.jsonl"), bufferDuration, ModeManual)
	if clientStatus.IsCaptured {
		frameEvidence, frameErr := scenarioDumpEvidence(ctx, filepath.Join(result.Directory, "client-memory.jsonl"))
		if frameErr != nil {
			return result, fmt.Errorf("captureFrameHash: %w", frameErr)
		}
		frameLines, frameErr := scenarioDumpLines(ctx, frameEvidence)
		if frameErr != nil {
			return result, fmt.Errorf("captureFrameRead: %w", frameErr)
		}
		clientLines := make([][]byte, len(frameLines))
		for index, line := range frameLines {
			clientLines[index] = []byte(line)
		}
		records, malformedCount := decodeClientTraceEvents(clientLines)
		clientBoundary, boundaryAt, isBoundaryFound := clientReplayBoundary(records, result.Request, clientStatus.RequestedAt)
		if isBoundaryFound && clientBoundary.Request == result.Request && !boundaryAt.IsZero() {
			result.Evidences = append(result.Evidences, frameEvidence)
			result.ClientEvidence = frameEvidence
			result.ClientLines = frameLines
		} else {
			clientStatus.IsCaptured = false
			clientStatus.Status = fmt.Sprintf("missing_exact_boundary; malformed=%d", malformedCount)
		}
	}
	client, err := readClientTail(clientDirectory, bufferDuration)
	if err != nil {
		return result, fmt.Errorf("captureTail: %w", err)
	}
	err = writeLines(filepath.Join(result.Directory, "client.jsonl"), client.lines)
	if err != nil {
		return result, fmt.Errorf("captureClient: %w", err)
	}
	clientEvidence, err := scenarioDumpEvidence(ctx, filepath.Join(result.Directory, "client.jsonl"))
	if err != nil {
		return result, fmt.Errorf("captureClientHash: %w", err)
	}
	result.Evidences = append(result.Evidences, clientEvidence)
	result.CompletedAt = time.Now().UTC()
	metadata := struct {
		Boundary                  string           `json:"boundary"`
		Request                   string           `json:"request"`
		StartedAt                 time.Time        `json:"started_at"`
		CompletedAt               time.Time        `json:"completed_at"`
		TrafficEventCount         int              `json:"traffic_event_count"`
		TrafficByteCount          int              `json:"traffic_byte_count"`
		DroppedEventCount         uint64           `json:"dropped_event_count"`
		ProtocolEvidence          ScenarioEvidence `json:"protocol_evidence"`
		ServerStateCapture        stateCapture     `json:"server_state_capture"`
		ClientStatus              string           `json:"client_status"`
		IsClientResponseAvailable bool             `json:"is_client_response_available"`
	}{req.Boundary, result.Request, result.StartedAt, result.CompletedAt,
		len(events), byteCount, droppedCount, protocolEvidence, stateStatus,
		clientStatus.Status, clientStatus.IsCaptured}
	err = writeJSON(filepath.Join(result.Directory, "capture-metadata.json"), metadata)
	if err != nil {
		return result, fmt.Errorf("captureMetadata: %w", err)
	}
	return result, nil
}

const maximumScenarioEvidenceByte int64 = 128 * 1024 * 1024

func scenarioDumpEvidence(ctx context.Context, path string) (ScenarioEvidence, error) {
	err := ctx.Err()
	if err != nil {
		return ScenarioEvidence{}, fmt.Errorf("evidenceContext: %w", err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return ScenarioEvidence{}, fmt.Errorf("evidenceStat: %w", err)
	}
	if !fi.Mode().IsRegular() || fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || fi.Size() > maximumScenarioEvidenceByte {
		return ScenarioEvidence{}, errors.New("scenario raw evidence must be bounded regular files")
	}
	r, err := os.Open(path)
	if err != nil {
		return ScenarioEvidence{}, fmt.Errorf("evidenceOpen: %w", err)
	}
	digest := sha256.New()
	count := int64(0)
	buffer := make([]byte, 64*1024)
	var copyErr error
	for {
		copyErr = ctx.Err()
		if copyErr != nil {
			break
		}
		readCount, readErr := r.Read(buffer)
		if readCount > 0 {
			writtenCount, writeErr := digest.Write(buffer[:readCount])
			count += int64(writtenCount)
			if writeErr != nil {
				copyErr = fmt.Errorf("digestWrite: %w", writeErr)
				break
			}
			if writtenCount != readCount || count > maximumScenarioEvidenceByte {
				copyErr = errors.New("scenario digest write or input exceeds evidence bound")
				break
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			copyErr = fmt.Errorf("digestRead: %w", readErr)
			break
		}
	}
	closeErr := r.Close()
	if copyErr != nil || closeErr != nil {
		return ScenarioEvidence{}, fmt.Errorf("evidenceRead: %w", errors.Join(copyErr, closeErr))
	}
	if count != fi.Size() || count > maximumScenarioEvidenceByte {
		return ScenarioEvidence{}, errors.New("scenario raw evidence changed size during hashing")
	}
	err = ctx.Err()
	if err != nil {
		return ScenarioEvidence{}, fmt.Errorf("evidenceHashed: %w", err)
	}
	return ScenarioEvidence{Path: path, Size: &count, SHA256: hex.EncodeToString(digest.Sum(nil))}, nil
}

func scenarioDumpLines(ctx context.Context, evidence ScenarioEvidence) ([]string, error) {
	r, err := os.Open(evidence.Path)
	if err != nil {
		return nil, fmt.Errorf("linesOpen: %w", err)
	}
	scanner := bufio.NewScanner(io.LimitReader(r, maximumScenarioEvidenceByte+1))
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	lines := []string{}
	for scanner.Scan() {
		if len(lines) >= 250000 {
			closeErr := r.Close()
			return nil, fmt.Errorf("linesBound: %w", errors.Join(errors.New("scenario client line count exceeds diagnostic bound"), closeErr))
		}
		err = ctx.Err()
		if err != nil {
			closeErr := r.Close()
			return nil, fmt.Errorf("linesContext: %w", errors.Join(err, closeErr))
		}
		lines = append(lines, scanner.Text())
	}
	scanErr := scanner.Err()
	closeErr := r.Close()
	if scanErr != nil || closeErr != nil {
		return nil, fmt.Errorf("linesRead: %w", errors.Join(scanErr, closeErr))
	}
	return lines, nil
}
