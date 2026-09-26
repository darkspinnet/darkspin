package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	clientProbePollInterval       = 100 * time.Millisecond
	automaticNPCDriftDistance     = 5.0
	automaticNPCDriftSampleCount  = 3
	automaticStateCaptureDeadline = 500 * time.Millisecond
)

type clientObjectProbe struct {
	source          string
	networkObjectID uint32
	clientObjectID  uint32
	position        [3]float32
	timeMS          uint64
	line            int
	sampleAt        time.Time
	goal            *[3]float32
	partialGoal     *[3]float32
	flags           uint32
	targetHandle    uint32
	desiredStop     float32
}

type clientTraceSource struct {
	offset        int64
	pending       []byte
	objectMapper  clientObjectMapper
	isInitialized bool
	lineCount     int
}

type clientProbeMonitor struct {
	sourcesByPath map[string]*clientTraceSource
}

type clientObjectDriftFlow struct {
	clientPosition [3]float32
	outlierCount   uint8
	isInitialized  bool
	firstOutlierAt time.Time
	lastSampleAt   time.Time
}

type serverNPCProbe struct {
	actor  Actor
	object ObjectState
}

func newClientProbeMonitor() *clientProbeMonitor {
	return &clientProbeMonitor{
		sourcesByPath: make(map[string]*clientTraceSource),
	}
}

func (e *clientProbeMonitor) poll(traceDirectory string) ([]clientObjectProbe, error) {
	if strings.TrimSpace(traceDirectory) == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(traceDirectory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("probeDirectory: %w", err)
	}
	probes := make([]clientObjectProbe, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") ||
			strings.EqualFold(entry.Name(), "server.jsonl") {
			continue
		}
		path := filepath.Join(traceDirectory, entry.Name())
		source := e.sourcesByPath[path]
		if source == nil {
			source = &clientTraceSource{
				objectMapper: clientObjectMapper{
					networkObjectIDsByHandle: make(map[uint32]uint32),
					provenanceByHandle:       make(map[uint32]string),
				},
			}
			e.sourcesByPath[path] = source
		}
		currentProbes, pollErr := source.poll(path)
		if pollErr != nil {
			return nil, fmt.Errorf("probeSource[%s]: %w", entry.Name(), pollErr)
		}
		probes = append(probes, currentProbes...)
	}
	return probes, nil
}

func (e *clientTraceSource) poll(path string) ([]clientObjectProbe, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("probeStat: %w", err)
	}
	if fi.Size() < e.offset {
		e.offset = 0
		e.pending = nil
		e.objectMapper = clientObjectMapper{
			networkObjectIDsByHandle: make(map[uint32]uint32),
			provenanceByHandle:       make(map[uint32]string),
		}
		e.isInitialized = false
		e.lineCount = 0
	}
	if time.Since(fi.ModTime()) > 5*time.Second {
		return nil, nil
	}
	r, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("probeOpen: %w", err)
	}
	_, err = r.Seek(e.offset, io.SeekStart)
	if err != nil {
		closeErr := r.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("probeSeek: %w; close: %v", err, closeErr)
		}
		return nil, fmt.Errorf("probeSeek: %w", err)
	}
	contents, err := io.ReadAll(r)
	if err != nil {
		closeErr := r.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("probeRead: %w; close: %v", err, closeErr)
		}
		return nil, fmt.Errorf("probeRead: %w", err)
	}
	err = r.Close()
	if err != nil {
		return nil, fmt.Errorf("probeClose: %w", err)
	}
	e.offset += int64(len(contents))
	wasInitialized := e.isInitialized
	e.isInitialized = true
	if len(contents) == 0 {
		return nil, nil
	}
	combined := make([]byte, 0, len(e.pending)+len(contents))
	combined = append(combined, e.pending...)
	combined = append(combined, contents...)
	lastNewline := bytes.LastIndexByte(combined, '\n')
	if lastNewline < 0 {
		e.pending = combined
		return nil, nil
	}
	e.pending = append(e.pending[:0], combined[lastNewline+1:]...)
	lines := bytes.Split(combined[:lastNewline], []byte{'\n'})
	probes := make([]clientObjectProbe, 0)
	for _, line := range lines {
		e.lineCount++
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		event := clientTraceEvent{}
		err = json.Unmarshal(line, &event)
		if err != nil || event.Kind == "" {
			continue
		}
		packetID, payloadObjectID := clientApplicationIdentity(event)
		networkObjectID := e.objectMapper.mapEvent(event, packetID, payloadObjectID)
		if !wasInitialized || event.Kind != "object_probe" ||
			networkObjectID == 0 || event.ObjectID == 0 || len(event.PositionBits) != 3 {
			continue
		}
		position, isValid := decodePosition(event.PositionBits)
		if !isValid {
			continue
		}
		probe := clientObjectProbe{
			source:          filepath.Base(path),
			networkObjectID: networkObjectID,
			clientObjectID:  event.ObjectID,
			position:        position,
			timeMS:          event.TimeMS,
			line:            e.lineCount, flags: event.LocomotionFlags,
			targetHandle: event.TargetObjectID,
		}
		if event.SampleUnixNano > 0 {
			probe.sampleAt = time.Unix(0, event.SampleUnixNano).UTC()
		}
		goal, isGoalValid := decodePosition(event.GoalBits)
		if isGoalValid {
			probe.goal = &goal
		}
		partialGoal, isPartialGoalValid := decodePosition(event.PartialGoalBits)
		if isPartialGoalValid {
			probe.partialGoal = &partialGoal
		}
		stop, isStopValid := decodePosition([]uint32{event.DesiredStopBits, 0, 0})
		if isStopValid {
			probe.desiredStop = stop[0]
		}
		probes = append(probes, probe)
	}
	return probes, nil
}

func (e *Service) pollClientObjectDrift() {
	probes, err := e.clientMonitor.poll(e.traceDirectory)
	if err != nil {
		e.logger.Printf("Sync Snapshot client drift probe failed: %v", err)
		return
	}
	if len(probes) == 0 {
		return
	}
	e.mu.Lock()
	mode := e.mode
	provider := e.provider
	e.mu.Unlock()
	if mode == ModeOff || provider == nil {
		return
	}
	requestedAt := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), automaticStateCaptureDeadline)
	state, err := provider.SyncSnapshot(ctx, StateRequest{})
	cancel()
	if err != nil {
		e.logger.Printf("Sync Snapshot authoritative drift probe failed: %v", err)
		return
	}
	requests := e.detectNPCDriftRequests(state, probes)
	if len(requests) == 0 {
		return
	}
	completedAt := time.Now().UTC()
	transport := e.captureTransportDiagnostics(state)
	for _, request := range requests {
		request.triggerState = &state
		request.triggerCapture = stateCapture{
			Status: "complete", RequestedAt: requestedAt, CompletedAt: completedAt,
			DurationMS: float64(completedAt.Sub(requestedAt)) / float64(time.Millisecond),
		}
		request.triggerTransport = transport
		e.retainAutomatic(request)
	}
}

func (e *Service) detectNPCDriftRequests(
	state StateFrame, probes []clientObjectProbe,
) []autoRequest {
	probeSources := make(map[string]struct{})
	for _, probe := range probes {
		probeSources[probe.source] = struct{}{}
	}
	// A client trace source cannot be assigned to one server-side peer until
	// the capture carries an explicit connection identity. Avoid comparing one
	// client's probes with another player's authoritative state in co-op.
	if len(probeSources) != 1 || len(state.Sessions) != 1 {
		return nil
	}
	latestProbesByObjectID := make(map[uint32]clientObjectProbe)
	for _, probe := range probes {
		current, isFound := latestProbesByObjectID[probe.networkObjectID]
		if isFound && current.timeMS > probe.timeMS {
			continue
		}
		latestProbesByObjectID[probe.networkObjectID] = probe
	}
	npcsByObjectID := make(map[uint32]serverNPCProbe)
	for _, session := range state.Sessions {
		actor := Actor{
			UserID: int64(session.UserID), GameID: session.GameID,
			Remote: session.Remote,
		}
		for _, object := range session.Objects {
			if (object.Kind != "npc" && object.Kind != "hero" &&
				object.Kind != "companion" && object.Kind != "projectile") ||
				object.ObjectID == 0 || object.IsDefeated ||
				!object.IsPublished {
				continue
			}
			if _, isFound := npcsByObjectID[object.ObjectID]; isFound {
				continue
			}
			npcsByObjectID[object.ObjectID] = serverNPCProbe{actor: actor, object: object}
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.mode == ModeOff {
		return nil
	}
	requests := make([]autoRequest, 0, 1)
	for objectID, npc := range npcsByObjectID {
		if isClientOrbitPresentedNPC(npc.object.NounName) {
			continue
		}
		probe, isFound := latestProbesByObjectID[objectID]
		if !isFound {
			continue
		}
		sample := newMovementSample(state, state.Sessions[0], npc.object, probe)
		e.retainMovementLocked(sample)
		if e.mode != ModeAuto {
			continue
		}
		key := movementIdentity(sample)
		fingerprint := key + ":object-position-drift"
		if !sample.IsTimingValid {
			delete(e.clientObjectDriftFlows, key)
			continue
		}
		distance := sample.Distance
		if distance <= sample.AllowedDistance {
			e.resolveAutomaticIncidentLocked(fingerprint, state.CapturedAt)
			delete(e.clientObjectDriftFlows, key)
			continue
		}
		flow := e.clientObjectDriftFlows[key]
		if flow.firstOutlierAt.IsZero() || state.CapturedAt.Sub(flow.lastSampleAt) > time.Second {
			flow = clientObjectDriftFlow{firstOutlierAt: state.CapturedAt}
		}
		flow.lastSampleAt = state.CapturedAt
		flow.clientPosition = probe.position
		flow.isInitialized = true
		if flow.outlierCount < ^uint8(0) {
			flow.outlierCount++
		}
		e.clientObjectDriftFlows[key] = flow
		if flow.outlierCount < automaticNPCDriftSampleCount {
			continue
		}
		if distance <= automaticNPCDriftDistance && state.CapturedAt.Sub(flow.firstOutlierAt) < 2*time.Second {
			continue
		}
		contextText := fmt.Sprintf(
			"client/server %s position divergence object=%d noun=%q distance=%.2f allowance=%.2f client_handle=%d",
			npc.object.Kind, objectID, npc.object.NounName, distance, sample.AllowedDistance,
			probe.clientObjectID,
		)
		requests = append(requests, autoRequest{anomaly: anomaly{
			Actor: npc.actor, Context: contextText,
			Fingerprint: fingerprint, ObjectID: objectID, IsPersistent: true,
			TriggeredAt: state.CapturedAt, Detector: "object_position_drift",
			Threshold: sample.AllowedDistance, Measured: distance,
		}})
	}
	e.pruneMovementsLocked(state.CapturedAt)
	for key, flow := range e.clientObjectDriftFlows {
		if state.CapturedAt.Sub(flow.lastSampleAt) > time.Second {
			delete(e.clientObjectDriftFlows, key)
		}
	}
	return requests
}

func isClientOrbitPresentedNPC(nounName string) bool {
	return strings.EqualFold(nounName, "NomadDrone") ||
		strings.EqualFold(nounName, "NomadDrone.Noun")
}
