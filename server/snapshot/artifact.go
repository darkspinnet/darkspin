package snapshot

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type dumpRequest struct {
	Actor       Actor
	Trigger     string
	Context     string
	Fingerprint string
	ObjectID    uint32
}

type dumpResult struct {
	ID          string
	Directory   string
	ArchiveName string
	Actor       Actor
}

type manifest struct {
	FormatVersion                  uint32     `json:"format_version"`
	ID                             string     `json:"id"`
	CreatedAt                      time.Time  `json:"created_at"`
	BuildVersion                   string     `json:"build_version,omitempty"`
	Trigger                        string     `json:"trigger"`
	Context                        string     `json:"context"`
	Fingerprint                    string     `json:"fingerprint,omitempty"`
	ObjectID                       uint32     `json:"object_id,omitempty"`
	Actor                          Actor      `json:"actor"`
	Mode                           Mode       `json:"mode"`
	BufferDuration                 string     `json:"buffer_duration"`
	Delay                          string     `json:"automatic_delay"`
	WindowStartedAt                time.Time  `json:"window_started_at"`
	WindowEndedAt                  time.Time  `json:"window_ended_at"`
	TrafficEventCount              int        `json:"traffic_event_count"`
	TrafficByteCount               int        `json:"traffic_byte_count"`
	DroppedEventCount              uint64     `json:"dropped_event_count"`
	ClientLineCount                int        `json:"client_line_count"`
	IsClientMemoryCaptured         bool       `json:"is_client_memory_captured"`
	ClientBoundaryAt               *time.Time `json:"client_boundary_at,omitempty"`
	ClientRingLineCount            uint64     `json:"client_ring_line_count,omitempty"`
	ClientRingByteCount            uint64     `json:"client_ring_byte_count,omitempty"`
	ClientCapacityDroppedLineCount uint64     `json:"client_capacity_dropped_line_count,omitempty"`
	ClientCapacityDroppedByteCount uint64     `json:"client_capacity_dropped_byte_count,omitempty"`
	IsClientRingTruncated          bool       `json:"is_client_ring_truncated,omitempty"`
	ReplayEventCount               int        `json:"replay_event_count"`
	ClientReplayEventCount         int        `json:"client_replay_event_count"`
	ClientMalformedCount           int        `json:"client_malformed_line_count"`
	IsClientTimelineAligned        bool       `json:"is_client_timeline_aligned"`
	FindingCount                   int        `json:"finding_count"`
	LikelyCause                    string     `json:"likely_cause,omitempty"`
	Confidence                     string     `json:"confidence,omitempty"`
	ArchiveName                    string     `json:"archive_name,omitempty"`
	ClientSources                  []string   `json:"client_sources,omitempty"`
	Files                          []fileInfo `json:"files"`
}

type fileInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

type clientTail struct {
	lines   [][]byte
	sources []string
}

type clientCapture struct {
	IsCaptured  bool
	RequestedAt time.Time
}

func (e *Service) dump(ctx context.Context, req dumpRequest) (dumpResult, error) {
	err := ctx.Err()
	if err != nil {
		return dumpResult{}, fmt.Errorf("dumpContext: %w", err)
	}
	now := time.Now().UTC()
	id := fmt.Sprintf("SS-%06d", e.nextIncident.Add(1))
	e.mu.Lock()
	e.pruneLocked(now)
	events := cloneTrafficEvents(e.events)
	mode := e.mode
	bufferDuration := e.bufferDuration
	delay := e.delay
	droppedEventCount := uint64(len(e.droppedEventTimes))
	provider := e.provider
	e.mu.Unlock()

	state := StateFrame{CapturedAt: now, Actor: req.Actor}
	if provider != nil {
		stateResult, stateErr := provider.SyncSnapshot(ctx, StateRequest{Actor: req.Actor})
		if stateErr != nil {
			return dumpResult{}, fmt.Errorf("stateCapture: %w", stateErr)
		}
		state = stateResult
		if state.CapturedAt.IsZero() {
			state.CapturedAt = now
		}
		if state.Actor.UserID != 0 || state.Actor.Remote != "" {
			req.Actor = state.Actor
		}
	}
	if req.Actor.UserName == "" && req.Actor.UserID != 0 && e.resolveUserName != nil {
		req.Actor.UserName = e.resolveUserName(req.Actor.UserID)
		state.Actor.UserName = req.Actor.UserName
	}

	directoryName := id + "_" + now.Format("20060102T150405.000000000Z")
	directory := filepath.Join(e.directory, directoryName)
	err = os.MkdirAll(directory, 0o755)
	if err != nil {
		return dumpResult{}, fmt.Errorf("snapshotDirectory: %w", err)
	}
	trafficPath := filepath.Join(directory, "raknet.jsonl")
	trafficByteCount, err := writeTraffic(trafficPath, events)
	if err != nil {
		return dumpResult{}, fmt.Errorf("trafficWrite: %w", err)
	}
	statePath := filepath.Join(directory, "server-state.json")
	err = writeJSON(statePath, state)
	if err != nil {
		return dumpResult{}, fmt.Errorf("stateWrite: %w", err)
	}
	clientMemoryPath := filepath.Join(directory, "client-memory.jsonl")
	clientCaptureResult := e.requestClientMemory(
		ctx, directoryName, clientMemoryPath, bufferDuration, mode,
	)

	client, err := readClientTail(e.traceDirectory, bufferDuration)
	if err != nil {
		return dumpResult{}, fmt.Errorf("clientTail: %w", err)
	}
	clientPath := filepath.Join(directory, "client.jsonl")
	err = writeLines(clientPath, client.lines)
	if err != nil {
		return dumpResult{}, fmt.Errorf("clientWrite: %w", err)
	}
	timelineClientLines := client.lines
	timelineClientFile := "client.jsonl"
	if clientCaptureResult.IsCaptured {
		clientMemoryLines, readErr := readSnapshotLines(clientMemoryPath)
		if readErr != nil {
			return dumpResult{}, fmt.Errorf("clientMemoryRead: %w", readErr)
		}
		clientRecords, malformedCount := decodeClientTraceEvents(clientMemoryLines)
		_, _, isBoundaryFound := clientReplayBoundary(
			clientRecords, directoryName, clientCaptureResult.RequestedAt,
		)
		if isBoundaryFound {
			timelineClientLines = clientMemoryLines
			timelineClientFile = "client-memory.jsonl"
		} else {
			clientCaptureResult.IsCaptured = false
			e.logger.Printf(
				"Sync Snapshot client response ignored request=%s malformed=%d: matching boundary unavailable",
				directoryName, malformedCount,
			)
		}
	}
	boundaryAt := clientCaptureResult.RequestedAt
	if boundaryAt.IsZero() {
		boundaryAt = now
	}
	timeline := buildReplayTimeline(
		events, timelineClientLines, timelineClientFile, directoryName, boundaryAt,
	)
	timelinePath := filepath.Join(directory, "timeline.jsonl")
	err = writeReplayTimeline(timelinePath, timeline.Events)
	if err != nil {
		return dumpResult{}, fmt.Errorf("timelineWrite: %w", err)
	}
	analysis := analyzeSnapshot(
		id, req, state, events, timeline, clientCaptureResult, droppedEventCount, now,
	)
	analysisPath := filepath.Join(directory, "analysis.json")
	err = writeJSON(analysisPath, analysis)
	if err != nil {
		return dumpResult{}, fmt.Errorf("analysisWrite: %w", err)
	}
	reportPath := filepath.Join(directory, "report.md")
	err = writeAnalysisMarkdown(reportPath, analysis)
	if err != nil {
		return dumpResult{}, fmt.Errorf("reportWrite: %w", err)
	}

	windowStartedAt := now.Add(-bufferDuration)
	if len(events) != 0 {
		windowStartedAt = events[0].CapturedAt
	}
	files := []fileInfo{
		{Name: "raknet.jsonl", Description: "Exact server-observed RakNet datagrams and delivery-ordered client application payloads"},
		{Name: "server-state.json", Description: "Authoritative gameplay object, squad, ability runtime, encounter, director, interaction, command admission, motion, cooldown, lifecycle, objective, deterministic-random, and exact pending-output keyframe at snapshot time"},
		{Name: "client.jsonl", Description: "Matching tail of observational Fang client packet and memory state events"},
		{Name: "timeline.jsonl", Description: "Clock-aligned replay index joining server transport, application delivery, client frame/action state, and RakNet-associated client object state"},
		{Name: "analysis.json", Description: "Machine-readable transport, pending-output, action/input, lifecycle, delivery, projectile-position, and client locomotion-stall comparisons"},
		{Name: "report.md", Description: "Readable root-cause summary with cited source lines and next checks"},
	}
	if clientCaptureResult.IsCaptured {
		files = append(files, fileInfo{
			Name:        "client-memory.jsonl",
			Description: "Exact client in-memory packet, frame, action/input, object, and locomotion buffer dumped by Fang at the snapshot boundary",
		})
	}
	files, err = hashFileInfos(directory, files)
	if err != nil {
		return dumpResult{}, fmt.Errorf("fileHash: %w", err)
	}
	archiveName := ""
	if strings.TrimSpace(e.archiveDirectory) != "" {
		archiveName = snapshotArchiveName(id, e.buildVersion, now)
	}
	metadata := manifest{
		FormatVersion: 13, ID: id, CreatedAt: now,
		BuildVersion: e.buildVersion, Trigger: req.Trigger, Context: req.Context,
		Fingerprint: req.Fingerprint, ObjectID: req.ObjectID, Actor: req.Actor,
		Mode: mode, BufferDuration: bufferDuration.String(), Delay: delay.String(),
		WindowStartedAt: windowStartedAt, WindowEndedAt: now,
		TrafficEventCount: len(events), TrafficByteCount: trafficByteCount,
		DroppedEventCount: droppedEventCount, ClientLineCount: len(client.lines),
		IsClientMemoryCaptured:         clientCaptureResult.IsCaptured,
		ClientRingLineCount:            timeline.ClientRingLineCount,
		ClientRingByteCount:            timeline.ClientRingByteCount,
		ClientCapacityDroppedLineCount: timeline.ClientCapacityDroppedLineCount,
		ClientCapacityDroppedByteCount: timeline.ClientCapacityDroppedByteCount,
		IsClientRingTruncated:          timeline.IsClientRingTruncated,
		ReplayEventCount:               len(timeline.Events),
		ClientReplayEventCount:         timeline.ClientEventCount,
		ClientMalformedCount:           timeline.ClientMalformedLineCount,
		IsClientTimelineAligned:        timeline.IsClientAligned,
		FindingCount:                   len(analysis.Findings), LikelyCause: analysis.LikelyCause,
		Confidence: analysis.Confidence, ArchiveName: archiveName,
		ClientSources: client.sources,
		Files:         files,
	}
	if !clientCaptureResult.RequestedAt.IsZero() {
		boundaryCopy := clientCaptureResult.RequestedAt
		metadata.ClientBoundaryAt = &boundaryCopy
	}
	manifestPath := filepath.Join(directory, "manifest.json")
	err = writeJSON(manifestPath, metadata)
	if err != nil {
		return dumpResult{}, fmt.Errorf("manifestWrite: %w", err)
	}
	if archiveName != "" {
		err = writeSnapshotArchive(
			e.archiveDirectory, directory, directoryName, archiveName,
		)
		if err != nil {
			return dumpResult{}, fmt.Errorf("archiveWrite: %w", err)
		}
	}
	e.mu.Lock()
	e.incidentsByID[id] = incident{ID: id, Fingerprint: req.Fingerprint}
	e.mu.Unlock()
	return dumpResult{
		ID: id, Directory: directory, ArchiveName: archiveName, Actor: req.Actor,
	}, nil
}

func snapshotArchiveName(id string, buildVersion string, createdAt time.Time) string {
	version := snapshotArchiveSegment(buildVersion)
	return fmt.Sprintf(
		"%s-darkspin-snapshot-%s-%s.zip", id, version,
		createdAt.UTC().Format("20060102T150405.000000000Z"),
	)
}

func snapshotArchiveSegment(raw string) string {
	var segment strings.Builder
	for _, character := range strings.TrimSpace(raw) {
		if unicode.IsLetter(character) || unicode.IsNumber(character) ||
			character == '.' || character == '-' || character == '_' {
			segment.WriteRune(character)
			continue
		}
		segment.WriteByte('-')
	}
	normalized := strings.Trim(segment.String(), ".-")
	if normalized == "" {
		return "unknown"
	}
	return normalized
}

func writeSnapshotArchive(
	archiveDirectory string, bundleDirectory string, bundleName string,
	archiveName string,
) error {
	err := os.MkdirAll(archiveDirectory, 0o755)
	if err != nil {
		return fmt.Errorf("archiveMkdir: %w", err)
	}
	output, err := os.CreateTemp(archiveDirectory, ".snapshot-*.zip")
	if err != nil {
		return fmt.Errorf("archiveTemp: %w", err)
	}
	temporaryPath := output.Name()
	isComplete := false
	defer func() {
		if isComplete {
			return
		}
		closeErr := output.Close()
		if closeErr != nil {
			// Cleanup continues because the failed archive must never be published.
		}
		removeErr := os.Remove(temporaryPath)
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			// The original archive failure remains the actionable error.
		}
	}()

	entries, err := os.ReadDir(bundleDirectory)
	if err != nil {
		return fmt.Errorf("bundleRead: %w", err)
	}
	archive := zip.NewWriter(output)
	for index, entry := range entries {
		err = addSnapshotArchiveFile(
			archive, bundleDirectory, bundleName, entry,
		)
		if err != nil {
			archiveCloseErr := archive.Close()
			if archiveCloseErr != nil {
				return fmt.Errorf(
					"archiveFile[%d]: %w; archiveClose: %v",
					index, err, archiveCloseErr,
				)
			}
			return fmt.Errorf("archiveFile[%d]: %w", index, err)
		}
	}
	err = archive.Close()
	if err != nil {
		return fmt.Errorf("archiveClose: %w", err)
	}
	err = output.Close()
	if err != nil {
		return fmt.Errorf("outputClose: %w", err)
	}
	archivePath := filepath.Join(archiveDirectory, archiveName)
	err = os.Rename(temporaryPath, archivePath)
	if err != nil {
		return fmt.Errorf("archiveRename: %w", err)
	}
	isComplete = true
	return nil
}

func addSnapshotArchiveFile(
	archive *zip.Writer, bundleDirectory string, bundleName string,
	entry os.DirEntry,
) error {
	fi, err := entry.Info()
	if err != nil {
		return fmt.Errorf("fileInfo: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("fileMode: %s is not regular", entry.Name())
	}
	header, err := zip.FileInfoHeader(fi)
	if err != nil {
		return fmt.Errorf("headerCreate: %w", err)
	}
	header.Name = filepath.ToSlash(filepath.Join(bundleName, entry.Name()))
	header.Method = zip.Deflate
	w, err := archive.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("entryCreate: %w", err)
	}
	r, err := os.Open(filepath.Join(bundleDirectory, entry.Name()))
	if err != nil {
		return fmt.Errorf("entryOpen: %w", err)
	}
	_, err = io.Copy(w, r)
	if err != nil {
		closeErr := r.Close()
		if closeErr != nil {
			return fmt.Errorf("entryCopy: %w; entryClose: %v", err, closeErr)
		}
		return fmt.Errorf("entryCopy: %w", err)
	}
	err = r.Close()
	if err != nil {
		return fmt.Errorf("entryClose: %w", err)
	}
	return nil
}

func (e *Service) requestClientMemory(
	ctx context.Context, directoryName string, outputPath string,
	bufferDuration time.Duration, mode Mode,
) clientCapture {
	if strings.TrimSpace(e.controlPath) == "" || mode == ModeOff {
		return clientCapture{}
	}
	e.controlMu.Lock()
	defer e.controlMu.Unlock()
	requestedAt := time.Now().UTC()
	control := fmt.Sprintf(
		"mode=%s\nrequest=%s\nbuffer_ms=%d\nserver_time_unix_nano=%d\n",
		mode, directoryName, bufferDuration/time.Millisecond, requestedAt.UnixNano(),
	)
	err := os.WriteFile(e.controlPath, []byte(control), 0o644)
	if err != nil {
		e.logger.Printf("Sync Snapshot client request failed: %v", err)
		return clientCapture{RequestedAt: requestedAt}
	}
	deadline := time.Now().Add(750 * time.Millisecond)
	lastSize := int64(-1)
	stableCount := 0
	for time.Now().Before(deadline) {
		fi, statErr := os.Stat(outputPath)
		err = statErr
		if err == nil {
			if fi.Size() == lastSize {
				stableCount++
			} else {
				lastSize = fi.Size()
				stableCount = 0
			}
			if stableCount >= 2 {
				return clientCapture{IsCaptured: true, RequestedAt: requestedAt}
			}
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			e.logger.Printf("Sync Snapshot client response failed: %v", err)
			return clientCapture{RequestedAt: requestedAt}
		}
		if ctx.Err() != nil {
			return clientCapture{RequestedAt: requestedAt}
		}
		time.Sleep(25 * time.Millisecond)
	}
	return clientCapture{RequestedAt: requestedAt}
}

func readSnapshotLines(path string) ([][]byte, error) {
	r, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("snapshotOpen: %w", err)
	}
	defer func() {
		closeErr := r.Close()
		if closeErr != nil {
			// The immutable diagnostic file was already consumed by the scanner.
		}
	}()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	lines := make([][]byte, 0, 1024)
	for scanner.Scan() {
		lines = append(lines, append([]byte(nil), scanner.Bytes()...))
	}
	err = scanner.Err()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("snapshotScan: %w", err)
	}
	return lines, nil
}

func cloneTrafficEvents(events []trafficEvent) []trafficEvent {
	clones := make([]trafficEvent, len(events))
	for index, event := range events {
		clones[index] = event
		clones[index].Payload = append([]byte(nil), event.Payload...)
	}
	return clones
}

func writeTraffic(path string, events []trafficEvent) (int, error) {
	w, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("trafficCreate: %w", err)
	}
	encoder := json.NewEncoder(w)
	payloadByteCount := 0
	for index, event := range events {
		err = encoder.Encode(event)
		if err != nil {
			closeErr := w.Close()
			if closeErr != nil {
				return 0, fmt.Errorf("trafficEncode[%d]: %w; close: %v", index, err, closeErr)
			}
			return 0, fmt.Errorf("trafficEncode[%d]: %w", index, err)
		}
		payloadByteCount += len(event.Payload)
	}
	err = w.Close()
	if err != nil {
		return 0, fmt.Errorf("trafficClose: %w", err)
	}
	return payloadByteCount, nil
}

func writeJSON(path string, document any) error {
	contents, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("jsonMarshal: %w", err)
	}
	contents = append(contents, '\n')
	err = os.WriteFile(path, contents, 0o644)
	if err != nil {
		return fmt.Errorf("jsonWrite: %w", err)
	}
	return nil
}

func writeLines(path string, lines [][]byte) error {
	w, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("lineCreate: %w", err)
	}
	for index, line := range lines {
		_, err = w.Write(line)
		if err == nil && (len(line) == 0 || line[len(line)-1] != '\n') {
			_, err = w.Write([]byte{'\n'})
		}
		if err != nil {
			closeErr := w.Close()
			if closeErr != nil {
				return fmt.Errorf("lineWrite[%d]: %w; close: %v", index, err, closeErr)
			}
			return fmt.Errorf("lineWrite[%d]: %w", index, err)
		}
	}
	err = w.Close()
	if err != nil {
		return fmt.Errorf("lineClose: %w", err)
	}
	return nil
}

func readClientTail(traceDirectory string, duration time.Duration) (clientTail, error) {
	if strings.TrimSpace(traceDirectory) == "" {
		return clientTail{}, nil
	}
	entries, err := os.ReadDir(traceDirectory)
	if errors.Is(err, os.ErrNotExist) {
		return clientTail{}, nil
	}
	if err != nil {
		return clientTail{}, fmt.Errorf("traceRead: %w", err)
	}
	tail := clientTail{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") ||
			strings.EqualFold(entry.Name(), "server.jsonl") {
			continue
		}
		path := filepath.Join(traceDirectory, entry.Name())
		lines, readErr := readClientFileTail(path, duration)
		if readErr != nil {
			return clientTail{}, fmt.Errorf("clientRead[%s]: %w", entry.Name(), readErr)
		}
		if len(lines) == 0 {
			continue
		}
		tail.lines = append(tail.lines, lines...)
		tail.sources = append(tail.sources, entry.Name())
	}
	sort.Strings(tail.sources)
	return tail, nil
}

type timedClientLine struct {
	timeMS uint64
	line   []byte
}

func readClientFileTail(path string, duration time.Duration) ([][]byte, error) {
	r, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("clientOpen: %w", err)
	}
	defer func() {
		closeErr := r.Close()
		if closeErr != nil {
			// The scanner has already consumed the immutable diagnostic source.
		}
	}()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	lines := make([]timedClientLine, 0, 1024)
	latestTimeMS := uint64(0)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		timeMS, isFound := clientTimeMS(line)
		if !isFound {
			continue
		}
		lines = append(lines, timedClientLine{timeMS: timeMS, line: line})
		if timeMS > latestTimeMS {
			latestTimeMS = timeMS
		}
	}
	err = scanner.Err()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("clientScan: %w", err)
	}
	durationMS := uint64(duration / time.Millisecond)
	cutoff := uint64(0)
	if latestTimeMS > durationMS {
		cutoff = latestTimeMS - durationMS
	}
	selected := make([][]byte, 0, len(lines))
	for _, line := range lines {
		if line.timeMS < cutoff {
			continue
		}
		selected = append(selected, line.line)
	}
	return selected, nil
}

func clientTimeMS(line []byte) (uint64, bool) {
	const marker = `"time_ms":`
	text := string(line)
	index := strings.Index(text, marker)
	if index < 0 {
		return 0, false
	}
	text = text[index+len(marker):]
	end := strings.IndexAny(text, ",}")
	if end < 0 {
		return 0, false
	}
	timeMS, err := strconv.ParseUint(strings.TrimSpace(text[:end]), 10, 64)
	if err != nil {
		return 0, false
	}
	return timeMS, true
}
