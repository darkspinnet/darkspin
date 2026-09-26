package snapshot

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darkspinnet/darkspin/server/chat"
	"github.com/darkspinnet/darkspin/server/raknet"
)

type Options struct {
	Directory        string
	ArchiveDirectory string
	TraceDirectory   string
	ControlPath      string
	Mode             Mode
	BufferDuration   time.Duration
	Delay            time.Duration
	BuildVersion     string
	Logger           *log.Logger
	ResolveUserName  func(int64) string
}

type trafficEvent struct {
	Sequence         uint64                      `json:"sequence"`
	CapturedAt       time.Time                   `json:"captured_at"`
	Direction        raknet.ObservationDirection `json:"direction"`
	Stage            raknet.ObservationStage     `json:"stage"`
	Remote           string                      `json:"remote,omitempty"`
	TraceID          uint64                      `json:"trace_id,omitempty"`
	PeerGeneration   uint64                      `json:"peer_generation,omitempty"`
	DatagramSequence uint32                      `json:"datagram_sequence,omitempty"`
	Reliability      raknet.Reliability          `json:"reliability,omitempty"`
	MessageIndex     uint32                      `json:"message_index,omitempty"`
	OrderIndex       uint32                      `json:"order_index,omitempty"`
	OrderChannel     uint8                       `json:"order_channel,omitempty"`
	IsSplit          bool                        `json:"is_split,omitempty"`
	SplitCount       uint32                      `json:"split_count,omitempty"`
	SplitIndex       uint32                      `json:"split_index,omitempty"`
	SplitID          uint16                      `json:"split_id,omitempty"`
	PacketID         uint8                       `json:"packet_id,omitempty"`
	ObjectID         uint32                      `json:"object_id,omitempty"`
	Payload          []byte                      `json:"payload"`
}

type objectFlow struct {
	isCreated      bool
	isMoveStarted  bool
	isDeleted      bool
	createdAt      time.Time
	lastMovementAt time.Time
}

type clientFlow struct {
	position       raknet.Vector3
	lastReportedAt time.Time
	lastTeleportAt time.Time
	outlierCount   uint8
}

type anomaly struct {
	Actor         Actor
	Context       string
	Fingerprint   string
	ObjectID      uint32
	IsPersistent  bool
	TriggeredAt   time.Time
	Detector      string
	EventSequence uint64
	Threshold     float64
	Measured      float64
}

type incident struct {
	ID          string
	Fingerprint string
}

type autoRequest struct {
	anomaly          anomaly
	triggerState     *StateFrame
	triggerCapture   stateCapture
	triggerTransport []TransportDiagnosticsState
}

type automaticIncidentState string

const (
	automaticIncidentPending            automaticIncidentState = "pending"
	automaticIncidentCapturing          automaticIncidentState = "capturing"
	automaticIncidentCaptured           automaticIncidentState = "captured"
	automaticIncidentResolved           automaticIncidentState = "resolved"
	automaticIncidentEvicted            automaticIncidentState = "evicted"
	automaticIncidentIgnored            automaticIncidentState = "ignored"
	maximumAutomaticIncidentCount                              = 64
	maximumAutomaticIncidentRecordCount                        = 64
	maximumAutomaticRetryDelay                                 = 30 * time.Second
)

type automaticIncident struct {
	anomaly          anomaly
	state            automaticIncidentState
	firstObservedAt  time.Time
	lastObservedAt   time.Time
	resolvedAt       time.Time
	nextAttemptAt    time.Time
	observationCount uint64
	attemptCount     uint64
	suppressionCount uint64
	lastFailure      string
	triggerState     *StateFrame
	triggerCapture   stateCapture
	triggerTransport []TransportDiagnosticsState
	triggerMovements []movementSample
}

type automaticIncidentRecord struct {
	State            automaticIncidentState `json:"state"`
	Fingerprint      string                 `json:"fingerprint"`
	Context          string                 `json:"context"`
	Actor            Actor                  `json:"actor"`
	ObjectID         uint32                 `json:"object_id,omitempty"`
	FirstObservedAt  time.Time              `json:"first_observed_at"`
	LastObservedAt   time.Time              `json:"last_observed_at"`
	ResolvedAt       *time.Time             `json:"resolved_at,omitempty"`
	NextAttemptAt    *time.Time             `json:"next_attempt_at,omitempty"`
	ObservationCount uint64                 `json:"observation_count"`
	AttemptCount     uint64                 `json:"attempt_count"`
	SuppressionCount uint64                 `json:"suppression_count,omitempty"`
	LastFailure      string                 `json:"last_failure,omitempty"`
	TriggeredAt      time.Time              `json:"triggered_at,omitempty"`
	Detector         string                 `json:"detector,omitempty"`
	DetectorVersion  uint32                 `json:"detector_version,omitempty"`
	EventSequence    uint64                 `json:"event_sequence,omitempty"`
	Threshold        float64                `json:"threshold,omitempty"`
	Measured         float64                `json:"measured,omitempty"`
}

// Service retains bounded transport events and writes local incident bundles.
type Service struct {
	mu                       sync.Mutex
	controlMu                sync.Mutex
	directory                string
	archiveDirectory         string
	traceDirectory           string
	controlPath              string
	mode                     Mode
	bufferDuration           time.Duration
	delay                    time.Duration
	buildVersion             string
	logger                   *log.Logger
	resolveUserName          func(int64) string
	events                   []trafficEvent
	bufferedByte             int
	droppedEventTimes        []time.Time
	suppressedCount          uint64
	nextEventSequence        uint64
	nextIncident             atomic.Uint64
	lastAutomaticAt          time.Time
	isAutomaticDumpRunning   bool
	flowsByRemote            map[string]map[uint32]objectFlow
	clientFlowsByRemote      map[string]map[uint32]clientFlow
	clientObjectDriftFlows   map[string]clientObjectDriftFlow
	movements                []movementSample
	nextMovementSequence     uint64
	nackTimesByRemote        map[string][]time.Time
	incidentsByID            map[string]incident
	ignoredFingerprint       map[string]struct{}
	automaticIncidents       map[string]*automaticIncident
	automaticIncidentOrder   []string
	automaticIncidentRecords []automaticIncidentRecord
	evictedIncidentCount     uint64
	ignoredIncidentCount     uint64
	provider                 StateProvider
	transportProvider        TransportDiagnosticsProvider
	notifier                 Notifier
	clientMonitor            *clientProbeMonitor
	automaticWake            chan struct{}
	close                    chan struct{}
	closeOnce                sync.Once
}

// New creates one opt-in snapshot service and starts its bounded artifact worker.
func New(options Options) (*Service, error) {
	if strings.TrimSpace(options.Directory) == "" {
		return nil, errors.New("snapshot directory missing")
	}
	mode, err := ParseMode(string(options.Mode))
	if err != nil {
		return nil, fmt.Errorf("snapshotMode: %w", err)
	}
	bufferDuration := options.BufferDuration
	if bufferDuration <= 0 || bufferDuration > maximumBufferDuration {
		return nil, errors.New("snapshot buffer duration must be between 1 second and 5 minutes")
	}
	delay := options.Delay
	if delay < minimumDelay || delay > maximumDelay {
		return nil, errors.New("snapshot delay must be between 1 second and 1 hour")
	}
	logger := options.Logger
	if logger == nil {
		logger = log.Default()
	}
	service := &Service{
		directory: options.Directory, archiveDirectory: options.ArchiveDirectory,
		traceDirectory: options.TraceDirectory,
		controlPath:    options.ControlPath,
		mode:           mode, bufferDuration: bufferDuration, delay: delay,
		buildVersion: options.BuildVersion, logger: logger,
		resolveUserName:        options.ResolveUserName,
		flowsByRemote:          make(map[string]map[uint32]objectFlow),
		clientFlowsByRemote:    make(map[string]map[uint32]clientFlow),
		clientObjectDriftFlows: make(map[string]clientObjectDriftFlow),
		nackTimesByRemote:      make(map[string][]time.Time),
		incidentsByID:          make(map[string]incident),
		ignoredFingerprint:     make(map[string]struct{}),
		automaticIncidents:     make(map[string]*automaticIncident),
		clientMonitor:          newClientProbeMonitor(),
		automaticWake:          make(chan struct{}, 1), close: make(chan struct{}),
	}
	go service.run()
	service.writeControlMode(mode)
	return service, nil
}

// Close stops automatic artifact creation. It does not remove diagnostics.
func (e *Service) Close() {
	if e == nil {
		return
	}
	e.closeOnce.Do(func() { close(e.close) })
}

// UseStateProvider installs gameplay's authoritative keyframe provider.
func (e *Service) UseStateProvider(provider StateProvider) {
	e.mu.Lock()
	e.provider = provider
	e.mu.Unlock()
}

func (e *Service) UseTransportDiagnosticsProvider(provider TransportDiagnosticsProvider) {
	e.mu.Lock()
	e.transportProvider = provider
	e.mu.Unlock()
}

// UseNotifier installs the in-game automatic incident publisher.
func (e *Service) UseNotifier(notifier Notifier) {
	e.mu.Lock()
	e.notifier = notifier
	e.mu.Unlock()
}

// ObserveRakNet implements raknet.Observer. It only clones and classifies
// bounded memory; disk I/O is delegated to the artifact worker.
func (e *Service) ObserveRakNet(observation raknet.Observation) {
	if e == nil || len(observation.Payload) == 0 {
		return
	}
	e.mu.Lock()
	if e.mode == ModeOff {
		e.mu.Unlock()
		return
	}
	e.nextEventSequence++
	event := trafficEvent{
		Sequence: e.nextEventSequence, CapturedAt: observation.CapturedAt,
		Direction: observation.Direction, Stage: observation.Stage,
		Remote: observation.Remote, TraceID: observation.TraceID,
		PeerGeneration:   observation.PeerGeneration,
		DatagramSequence: observation.DatagramSequence,
		Reliability:      observation.Reliability, MessageIndex: observation.MessageIndex,
		OrderIndex: observation.OrderIndex, OrderChannel: observation.OrderChannel,
		IsSplit: observation.IsSplit, SplitCount: observation.SplitCount,
		SplitIndex: observation.SplitIndex, SplitID: observation.SplitID,
		Payload: append([]byte(nil), observation.Payload...),
	}
	event.PacketID, event.ObjectID = eventIdentity(event)
	e.events = append(e.events, event)
	e.bufferedByte += len(event.Payload)
	e.pruneLocked(observation.CapturedAt)
	requests := e.detectRequestsLocked(event)
	e.mu.Unlock()
	for _, request := range requests {
		e.retainAutomatic(request)
	}
}

func (e *Service) detectRequestsLocked(event trafficEvent) []autoRequest {
	request, isTriggered := e.detectLocked(event)
	if isTriggered {
		return []autoRequest{request}
	}
	if e.mode != ModeAuto || event.Direction != raknet.ObservationServerToClient ||
		event.Stage != raknet.ObservationDatagram || len(event.Payload) == 0 ||
		event.Payload[0]&0x80 == 0 || event.Payload[0] == 0xa0 ||
		event.Payload[0] == 0xc0 {
		return nil
	}
	datagram, err := raknet.DecodeDatagram(event.Payload)
	if err != nil || len(datagram.Packets) <= 1 {
		return nil
	}
	requests := make([]autoRequest, 0, 1)
	for _, packet := range datagram.Packets {
		if packet.IsSplit || len(packet.Payload) == 0 {
			continue
		}
		classified := event
		classified.Reliability = packet.Reliability
		classified.MessageIndex = packet.MessageIndex
		classified.OrderIndex = packet.OrderIndex
		classified.OrderChannel = packet.OrderChannel
		classified.Payload = packet.Payload
		classified.PacketID, classified.ObjectID = applicationIdentity(packet.Payload)
		request, isTriggered = e.detectLocked(classified)
		if isTriggered {
			requests = append(requests, request)
		}
	}
	return requests
}

func eventIdentity(event trafficEvent) (uint8, uint32) {
	payload := event.Payload
	if event.Stage == raknet.ObservationDatagram || event.Stage == raknet.ObservationRetransmit {
		if len(payload) == 0 || payload[0]&0x80 == 0 || payload[0] == 0xa0 || payload[0] == 0xc0 {
			return 0, 0
		}
		datagram, err := raknet.DecodeDatagram(payload)
		if err != nil || len(datagram.Packets) != 1 || datagram.Packets[0].IsSplit {
			return 0, 0
		}
		payload = datagram.Packets[0].Payload
	}
	return applicationIdentity(payload)
}

func applicationIdentity(payload []byte) (uint8, uint32) {
	if len(payload) == 0 {
		return 0, 0
	}
	packetID := payload[0]
	if raknet.PacketID(packetID) == raknet.ActionCommandMsgs && len(payload) > 1 {
		command, err := raknet.DecodeActionCommand(payload[1:])
		if err == nil {
			return packetID, command.Common.ObjectID
		}
	}
	if len(payload) < 5 || packetID < byte(raknet.ObjectCreate) {
		return packetID, 0
	}
	return packetID, binary.LittleEndian.Uint32(payload[1:5])
}

func (e *Service) pruneLocked(now time.Time) {
	cutoff := now.Add(-e.bufferDuration)
	droppedTimeCount := 0
	for droppedTimeCount < len(e.droppedEventTimes) &&
		e.droppedEventTimes[droppedTimeCount].Before(cutoff) {
		droppedTimeCount++
	}
	if droppedTimeCount != 0 {
		copy(e.droppedEventTimes, e.droppedEventTimes[droppedTimeCount:])
		e.droppedEventTimes = e.droppedEventTimes[:len(e.droppedEventTimes)-droppedTimeCount]
	}
	removeCount := 0
	for removeCount < len(e.events) && e.events[removeCount].CapturedAt.Before(cutoff) {
		current := e.events[removeCount]
		e.bufferedByte -= len(current.Payload)
		removeCount++
	}
	for removeCount < len(e.events) && e.bufferedByte > maximumBufferedByte {
		current := e.events[removeCount]
		e.bufferedByte -= len(current.Payload)
		e.droppedEventTimes = append(e.droppedEventTimes, current.CapturedAt)
		removeCount++
	}
	if removeCount == 0 {
		return
	}
	copy(e.events, e.events[removeCount:])
	e.events = e.events[:len(e.events)-removeCount]
}

func (e *Service) detectLocked(event trafficEvent) (autoRequest, bool) {
	if e.mode != ModeAuto {
		return autoRequest{}, false
	}
	if event.Direction == raknet.ObservationClientToServer {
		request, isTriggered := e.detectNACKBurstLocked(event)
		if isTriggered {
			return request, true
		}
		return e.detectClientDriftLocked(event)
	}
	if event.Direction != raknet.ObservationServerToClient {
		return autoRequest{}, false
	}
	if event.Stage != raknet.ObservationDatagram || event.PacketID == 0 {
		return autoRequest{}, false
	}
	flows := e.flowsByRemote[event.Remote]
	if flows == nil {
		flows = make(map[uint32]objectFlow)
		e.flowsByRemote[event.Remote] = flows
	}
	flow := flows[event.ObjectID]
	switch raknet.PacketID(event.PacketID) {
	case raknet.ObjectCreate:
		flow = objectFlow{isCreated: true, createdAt: event.CapturedAt}
		flows[event.ObjectID] = flow
	case raknet.ObjectPlayerMove:
		if flow.isCreated && !flow.isDeleted {
			flow.isMoveStarted = true
			flow.lastMovementAt = event.CapturedAt
			flows[event.ObjectID] = flow
		}
	case raknet.LocomotionUnreliable:
		if flow.isCreated && !flow.isDeleted && !flow.isMoveStarted &&
			event.CapturedAt.Sub(flow.createdAt) >= 100*time.Millisecond {
			return e.anomalyRequestLocked(event, "locomotion update arrived before movement intent"), true
		}
	case raknet.ObjectDelete:
		if flow.isCreated && flow.isDeleted {
			return e.anomalyRequestLocked(event, "duplicate object delete"), true
		}
		if flow.isCreated {
			flow.isDeleted = true
			flows[event.ObjectID] = flow
		}
	case raknet.ObjectTeleport:
		clientFlows := e.clientFlowsByRemote[event.Remote]
		if clientFlows == nil {
			clientFlows = make(map[uint32]clientFlow)
			e.clientFlowsByRemote[event.Remote] = clientFlows
		}
		clientFlow := clientFlows[event.ObjectID]
		clientFlow.lastTeleportAt = event.CapturedAt
		clientFlow.outlierCount = 0
		clientFlows[event.ObjectID] = clientFlow
	}
	return autoRequest{}, false
}

func (e *Service) detectNACKBurstLocked(event trafficEvent) (autoRequest, bool) {
	if event.Stage != raknet.ObservationDatagram || len(event.Payload) == 0 ||
		event.Payload[0] != 0xa0 {
		return autoRequest{}, false
	}
	cutoff := event.CapturedAt.Add(-2 * time.Second)
	times := e.nackTimesByRemote[event.Remote]
	kept := times[:0]
	for _, capturedAt := range times {
		if !capturedAt.Before(cutoff) {
			kept = append(kept, capturedAt)
		}
	}
	kept = append(kept, event.CapturedAt)
	e.nackTimesByRemote[event.Remote] = kept
	if len(kept) < 3 {
		return autoRequest{}, false
	}
	return autoRequest{anomaly: anomaly{
		Actor:       Actor{Remote: event.Remote},
		Context:     fmt.Sprintf("RakNet packet-loss burst: %d NACK datagrams in 2s", len(kept)),
		Fingerprint: event.Remote + ":nack-burst",
		TriggeredAt: event.CapturedAt, Detector: "nack_burst",
		EventSequence: event.Sequence, Threshold: 3, Measured: float64(len(kept)),
	}}, true
}

func (e *Service) detectClientDriftLocked(event trafficEvent) (autoRequest, bool) {
	if event.Stage != raknet.ObservationApplication ||
		raknet.PacketID(event.PacketID) != raknet.ActionCommandMsgs ||
		len(event.Payload) <= 1 {
		return autoRequest{}, false
	}
	command, err := raknet.DecodeActionCommand(event.Payload[1:])
	if err != nil || command.Common.ObjectID == 0 {
		return autoRequest{}, false
	}
	clientFlows := e.clientFlowsByRemote[event.Remote]
	if clientFlows == nil {
		clientFlows = make(map[uint32]clientFlow)
		e.clientFlowsByRemote[event.Remote] = clientFlows
	}
	flow := clientFlows[command.Common.ObjectID]
	position := command.Common.Position
	if flow.lastReportedAt.IsZero() {
		flow.position = position
		flow.lastReportedAt = event.CapturedAt
		clientFlows[command.Common.ObjectID] = flow
		return autoRequest{}, false
	}
	elapsed := event.CapturedAt.Sub(flow.lastReportedAt)
	distance := vectorDistance(flow.position, position)
	allowedDistance := float32(12)
	if elapsed > 0 {
		allowedDistance += float32(elapsed.Seconds()) * 20
	}
	isTeleportRecent := !flow.lastTeleportAt.IsZero() &&
		event.CapturedAt.Sub(flow.lastTeleportAt) <= 2*time.Second
	if elapsed <= 0 || elapsed > 5*time.Second || distance <= allowedDistance ||
		isTeleportRecent {
		flow.outlierCount = 0
	} else if flow.outlierCount < ^uint8(0) {
		flow.outlierCount++
	}
	flow.position = position
	flow.lastReportedAt = event.CapturedAt
	clientFlows[command.Common.ObjectID] = flow
	if flow.outlierCount < 2 {
		return autoRequest{}, false
	}
	contextText := fmt.Sprintf(
		"client movement drift object=%d distance=%.2f allowed=%.2f interval=%s",
		command.Common.ObjectID, distance, allowedDistance, elapsed.Round(time.Millisecond),
	)
	fingerprint := fmt.Sprintf("%s:movement-drift:%d", event.Remote, command.Common.ObjectID)
	return autoRequest{anomaly: anomaly{
		Actor: Actor{Remote: event.Remote}, Context: contextText,
		Fingerprint: fingerprint, ObjectID: command.Common.ObjectID,
		TriggeredAt: event.CapturedAt, Detector: "client_movement_drift",
		EventSequence: event.Sequence, Threshold: float64(allowedDistance),
		Measured: float64(distance),
	}}, true
}

func vectorDistance(left raknet.Vector3, right raknet.Vector3) float32 {
	x := float64(right.X - left.X)
	y := float64(right.Y - left.Y)
	z := float64(right.Z - left.Z)
	return float32(math.Sqrt(x*x + y*y + z*z))
}

func (e *Service) anomalyRequestLocked(event trafficEvent, contextText string) autoRequest {
	fingerprint := fmt.Sprintf("%s:%02x:%d", event.Remote, event.PacketID, event.ObjectID)
	return autoRequest{anomaly: anomaly{
		Actor: Actor{Remote: event.Remote}, Context: contextText,
		Fingerprint: fingerprint, ObjectID: event.ObjectID,
		TriggeredAt: event.CapturedAt, Detector: "server_lifecycle",
		EventSequence: event.Sequence,
	}}
}

func (e *Service) run() {
	ticker := time.NewTicker(clientProbePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.close:
			return
		case <-ticker.C:
			e.pollClientObjectDrift()
			e.processAutomatic()
		case <-e.automaticWake:
			e.processAutomatic()
		}
	}
}

func (e *Service) retainAutomatic(request autoRequest) {
	now := time.Now().UTC()
	e.mu.Lock()
	if e.mode != ModeAuto {
		e.mu.Unlock()
		return
	}
	if _, isIgnored := e.ignoredFingerprint[request.anomaly.Fingerprint]; isIgnored {
		e.suppressedCount++
		e.ignoredIncidentCount++
		e.mu.Unlock()
		return
	}
	current := e.automaticIncidents[request.anomaly.Fingerprint]
	if current != nil {
		current.lastObservedAt = now
		current.observationCount++
		current.resolvedAt = time.Time{}
		if current.state == automaticIncidentResolved {
			current.state = automaticIncidentPending
		}
		if !e.lastAutomaticAt.IsZero() && now.Sub(e.lastAutomaticAt) < e.delay {
			current.suppressionCount++
			e.suppressedCount++
		}
		e.mu.Unlock()
		return
	}
	if len(e.automaticIncidents) >= maximumAutomaticIncidentCount {
		e.evictAutomaticIncidentLocked(now)
	}
	current = &automaticIncident{
		anomaly: request.anomaly, state: automaticIncidentPending,
		firstObservedAt: now, lastObservedAt: now, observationCount: 1,
		triggerState: request.triggerState, triggerCapture: request.triggerCapture,
		triggerTransport: request.triggerTransport,
	}
	if request.triggerState != nil {
		current.nextAttemptAt = now.Add(2 * time.Second)
		current.triggerMovements = incidentMovements(e.movements, request.anomaly)
	}
	if !e.lastAutomaticAt.IsZero() && now.Sub(e.lastAutomaticAt) < e.delay {
		current.suppressionCount = 1
		e.suppressedCount++
	}
	e.automaticIncidents[request.anomaly.Fingerprint] = current
	e.automaticIncidentOrder = append(e.automaticIncidentOrder, request.anomaly.Fingerprint)
	e.mu.Unlock()
	e.wakeAutomatic()
}

func (e *Service) wakeAutomatic() {
	select {
	case e.automaticWake <- struct{}{}:
	default:
	}
}

func (e *Service) processAutomatic() {
	now := time.Now().UTC()
	e.mu.Lock()
	if e.mode != ModeAuto || e.isAutomaticDumpRunning ||
		(!e.lastAutomaticAt.IsZero() && now.Sub(e.lastAutomaticAt) < e.delay) {
		e.mu.Unlock()
		return
	}
	var current *automaticIncident
	for _, fingerprint := range e.automaticIncidentOrder {
		candidate := e.automaticIncidents[fingerprint]
		if candidate == nil ||
			(candidate.state != automaticIncidentPending && candidate.state != automaticIncidentResolved) ||
			candidate.nextAttemptAt.After(now) {
			continue
		}
		current = candidate
		break
	}
	if current == nil {
		e.mu.Unlock()
		return
	}
	current.state = automaticIncidentCapturing
	current.attemptCount++
	e.isAutomaticDumpRunning = true
	request := autoRequest{anomaly: current.anomaly}
	isTriggerStateNeeded := current.triggerState == nil
	e.mu.Unlock()
	if isTriggerStateNeeded {
		triggerState, triggerCapture := e.captureState(
			context.Background(), request.anomaly.Actor,
		)
		triggerTransport := e.captureTransportDiagnostics(triggerState)
		e.mu.Lock()
		retained := e.automaticIncidents[request.anomaly.Fingerprint]
		if retained != nil {
			retained.triggerState = &triggerState
			retained.triggerCapture = triggerCapture
			retained.triggerTransport = triggerTransport
			retained.state = automaticIncidentPending
			retained.nextAttemptAt = time.Now().UTC().Add(2 * time.Second)
		}
		e.isAutomaticDumpRunning = false
		e.mu.Unlock()
		return
	}

	result, err := e.dump(context.Background(), dumpRequest{
		Actor: request.anomaly.Actor, Trigger: "automatic",
		Context: request.anomaly.Context, Fingerprint: request.anomaly.Fingerprint,
		ObjectID:     request.anomaly.ObjectID,
		TriggerState: current.triggerState, TriggerStateCapture: current.triggerCapture,
		TriggerTransportDiagnostics: current.triggerTransport,
		TriggerMovements:            current.triggerMovements,
		TriggeredAt:                 request.anomaly.TriggeredAt,
	})
	if err != nil {
		e.mu.Lock()
		e.isAutomaticDumpRunning = false
		retained := e.automaticIncidents[request.anomaly.Fingerprint]
		if retained != nil {
			retained.state = automaticIncidentPending
			if !retained.resolvedAt.IsZero() {
				retained.state = automaticIncidentResolved
			}
			retained.lastFailure = err.Error()
			retained.nextAttemptAt = time.Now().UTC().Add(automaticRetryDelay(retained.attemptCount))
		}
		e.mu.Unlock()
		e.logger.Printf("Sync Snapshot automatic dump failed: %v", err)
		return
	}
	message := fmt.Sprintf("Automatic Sync Snapshot %s created", result.ID)
	if result.Actor.UserName != "" {
		message += fmt.Sprintf(" for %s (%d)", result.Actor.UserName, result.Actor.UserID)
	}
	if result.ArchiveName != "" {
		message += " and zipped in darkspin/bugs"
	}
	message += ": " + request.anomaly.Context
	e.logger.Print(message)
	e.mu.Lock()
	e.isAutomaticDumpRunning = false
	e.lastAutomaticAt = time.Now().UTC()
	retained := e.automaticIncidents[request.anomaly.Fingerprint]
	if retained != nil {
		retained.state = automaticIncidentCaptured
		retained.lastFailure = ""
		retained.nextAttemptAt = time.Time{}
		if !retained.anomaly.IsPersistent || !retained.resolvedAt.IsZero() {
			e.removeAutomaticIncidentLocked(request.anomaly.Fingerprint)
		}
	}
	notifier := e.notifier
	e.mu.Unlock()
	if notifier == nil || result.Actor.UserID == 0 {
		return
	}
	err = notifier.NotifySnapshot(context.Background(), Notice{
		Actor: result.Actor, Message: message,
	})
	if err != nil {
		e.logger.Printf("Sync Snapshot chat notification failed: %v", err)
	}
}

func automaticRetryDelay(attemptCount uint64) time.Duration {
	delay := time.Second
	for attempt := uint64(1); attempt < attemptCount && delay < maximumAutomaticRetryDelay; attempt++ {
		delay *= 2
	}
	if delay > maximumAutomaticRetryDelay {
		return maximumAutomaticRetryDelay
	}
	return delay
}

func (e *Service) evictAutomaticIncidentLocked(now time.Time) {
	for index, fingerprint := range e.automaticIncidentOrder {
		current := e.automaticIncidents[fingerprint]
		if current == nil || current.state == automaticIncidentCapturing {
			continue
		}
		record := automaticIncidentRecordFrom(current)
		record.State = automaticIncidentEvicted
		if record.ResolvedAt == nil {
			resolvedAt := now
			record.ResolvedAt = &resolvedAt
		}
		e.automaticIncidentRecords = append(e.automaticIncidentRecords, record)
		if len(e.automaticIncidentRecords) > maximumAutomaticIncidentRecordCount {
			e.automaticIncidentRecords = e.automaticIncidentRecords[len(e.automaticIncidentRecords)-maximumAutomaticIncidentRecordCount:]
		}
		delete(e.automaticIncidents, fingerprint)
		copy(e.automaticIncidentOrder[index:], e.automaticIncidentOrder[index+1:])
		e.automaticIncidentOrder = e.automaticIncidentOrder[:len(e.automaticIncidentOrder)-1]
		e.evictedIncidentCount++
		e.suppressedCount++
		return
	}
}

func (e *Service) removeAutomaticIncidentLocked(fingerprint string) {
	delete(e.automaticIncidents, fingerprint)
	for index, current := range e.automaticIncidentOrder {
		if current != fingerprint {
			continue
		}
		copy(e.automaticIncidentOrder[index:], e.automaticIncidentOrder[index+1:])
		e.automaticIncidentOrder = e.automaticIncidentOrder[:len(e.automaticIncidentOrder)-1]
		return
	}
}

func (e *Service) resolveAutomaticIncidentLocked(fingerprint string, resolvedAt time.Time) {
	current := e.automaticIncidents[fingerprint]
	if current == nil {
		return
	}
	current.resolvedAt = resolvedAt
	if current.state == automaticIncidentCaptured {
		e.removeAutomaticIncidentLocked(fingerprint)
		return
	}
	if current.state == automaticIncidentPending {
		current.state = automaticIncidentResolved
	}
}

func automaticIncidentRecordFrom(current *automaticIncident) automaticIncidentRecord {
	record := automaticIncidentRecord{
		State: current.state, Fingerprint: current.anomaly.Fingerprint,
		Context: current.anomaly.Context, Actor: current.anomaly.Actor,
		ObjectID: current.anomaly.ObjectID, FirstObservedAt: current.firstObservedAt,
		LastObservedAt: current.lastObservedAt, ObservationCount: current.observationCount,
		AttemptCount: current.attemptCount, SuppressionCount: current.suppressionCount,
		LastFailure: current.lastFailure,
		TriggeredAt: current.anomaly.TriggeredAt, Detector: current.anomaly.Detector,
		DetectorVersion: 2, EventSequence: current.anomaly.EventSequence,
		Threshold: current.anomaly.Threshold, Measured: current.anomaly.Measured,
	}
	if !current.resolvedAt.IsZero() {
		resolvedAt := current.resolvedAt
		record.ResolvedAt = &resolvedAt
	}
	if !current.nextAttemptAt.IsZero() {
		nextAttemptAt := current.nextAttemptAt
		record.NextAttemptAt = &nextAttemptAt
	}
	return record
}

func (e *Service) automaticIncidentRecordsLocked() []automaticIncidentRecord {
	records := append([]automaticIncidentRecord(nil), e.automaticIncidentRecords...)
	for _, fingerprint := range e.automaticIncidentOrder {
		current := e.automaticIncidents[fingerprint]
		if current != nil {
			records = append(records, automaticIncidentRecordFrom(current))
		}
	}
	return records
}

// Execute implements chat.SnapshotManager.
func (e *Service) Execute(
	ctx context.Context, command chat.SnapshotCommand,
) (chat.SnapshotResult, error) {
	if e == nil {
		return chat.SnapshotResult{}, chat.ErrSnapshotUnavailable
	}
	action := strings.ToLower(strings.TrimSpace(command.Action))
	switch action {
	case "", "status":
		return chat.SnapshotResult{Message: e.statusMessage()}, nil
	case "mode":
		mode, err := ParseMode(command.Argument)
		if err != nil {
			return chat.SnapshotResult{Message: "Syntax: /ss mode [manual|auto|off]"}, nil
		}
		e.setMode(mode)
		return chat.SnapshotResult{Message: "Sync Snapshot session mode set to " + string(mode)}, nil
	case "delay":
		second, err := strconv.ParseUint(strings.TrimSpace(command.Argument), 10, 32)
		if err != nil || second == 0 || time.Duration(second)*time.Second > maximumDelay {
			return chat.SnapshotResult{Message: "Syntax: /ss delay <1-3600 seconds>"}, nil
		}
		e.mu.Lock()
		e.delay = time.Duration(second) * time.Second
		e.mu.Unlock()
		return chat.SnapshotResult{Message: fmt.Sprintf("Sync Snapshot automatic delay set to %ds", second)}, nil
	case "ignore":
		return chat.SnapshotResult{Message: e.ignore(command.Argument)}, nil
	case "dump":
		e.mu.Lock()
		mode := e.mode
		e.mu.Unlock()
		if mode == ModeOff {
			return chat.SnapshotResult{Message: "Sync Snapshot is off; use /ss mode manual or /ss mode auto first"}, nil
		}
		result, err := e.dump(ctx, dumpRequest{
			Actor:   Actor{UserID: command.Sender.ID, UserName: command.Sender.Name, GameID: command.GameID},
			Trigger: "manual", Context: "manual in-game request",
		})
		if err != nil {
			return chat.SnapshotResult{}, fmt.Errorf("snapshotDump: %w", err)
		}
		message := fmt.Sprintf(
			"Snapshot %s created for %s (%d): captured the last %s",
			result.ID, command.Sender.Name, command.Sender.ID, e.bufferDuration,
		)
		if result.ArchiveName != "" {
			message += "; ZIP darkspin/bugs/" + result.ArchiveName
		}
		return chat.SnapshotResult{Message: message}, nil
	default:
		return chat.SnapshotResult{Message: snapshotSyntax}, nil
	}
}

const snapshotSyntax = "Sync Snapshot commands: /ss mode [manual|auto|off] | /ss dump | /ss delay <seconds> | /ss ignore <snapshot-id>"

func (e *Service) statusMessage() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return fmt.Sprintf(
		"Sync Snapshot mode=%s buffer=%s delay=%s events=%d dropped=%d pending=%d suppressed=%d evicted=%d ignored=%d. %s",
		e.mode, e.bufferDuration, e.delay, len(e.events),
		len(e.droppedEventTimes), len(e.automaticIncidents), e.suppressedCount,
		e.evictedIncidentCount, e.ignoredIncidentCount, snapshotSyntax,
	)
}

func (e *Service) setMode(mode Mode) {
	e.mu.Lock()
	e.mode = mode
	if mode != ModeAuto {
		e.clientObjectDriftFlows = make(map[string]clientObjectDriftFlow)
	}
	if mode == ModeOff {
		e.movements = nil
		for _, fingerprint := range e.automaticIncidentOrder {
			current := e.automaticIncidents[fingerprint]
			if current == nil {
				continue
			}
			record := automaticIncidentRecordFrom(current)
			record.State = automaticIncidentEvicted
			e.automaticIncidentRecords = append(e.automaticIncidentRecords, record)
			e.evictedIncidentCount++
		}
		if len(e.automaticIncidentRecords) > maximumAutomaticIncidentRecordCount {
			e.automaticIncidentRecords = e.automaticIncidentRecords[len(e.automaticIncidentRecords)-maximumAutomaticIncidentRecordCount:]
		}
		e.automaticIncidents = make(map[string]*automaticIncident)
		e.automaticIncidentOrder = nil
		e.events = nil
		e.bufferedByte = 0
		e.droppedEventTimes = nil
		e.flowsByRemote = make(map[string]map[uint32]objectFlow)
		e.clientFlowsByRemote = make(map[string]map[uint32]clientFlow)
		e.nackTimesByRemote = make(map[string][]time.Time)
	}
	e.mu.Unlock()
	e.writeControlMode(mode)
}

func (e *Service) writeControlMode(mode Mode) {
	if strings.TrimSpace(e.controlPath) == "" {
		return
	}
	e.controlMu.Lock()
	defer e.controlMu.Unlock()
	err := os.WriteFile(
		e.controlPath, []byte("mode="+string(mode)+"\n"), 0o644,
	)
	if err != nil {
		e.logger.Printf("Sync Snapshot client mode update failed: %v", err)
	}
}

// SetMode applies a live launcher-selected collection mode.
func (e *Service) SetMode(mode Mode) {
	if e == nil {
		return
	}
	e.setMode(mode)
}

func (e *Service) ignore(rawID string) string {
	id := strings.ToUpper(strings.TrimSpace(rawID))
	if id != "" && !strings.HasPrefix(id, "SS-") {
		id = "SS-" + id
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	current, isFound := e.incidentsByID[id]
	if !isFound || current.Fingerprint == "" {
		return "Snapshot ID not found or was not created automatically"
	}
	e.ignoredFingerprint[current.Fingerprint] = struct{}{}
	retained := e.automaticIncidents[current.Fingerprint]
	if retained != nil {
		record := automaticIncidentRecordFrom(retained)
		record.State = automaticIncidentIgnored
		e.automaticIncidentRecords = append(e.automaticIncidentRecords, record)
		if len(e.automaticIncidentRecords) > maximumAutomaticIncidentRecordCount {
			e.automaticIncidentRecords = e.automaticIncidentRecords[len(e.automaticIncidentRecords)-maximumAutomaticIncidentRecordCount:]
		}
		e.removeAutomaticIncidentLocked(current.Fingerprint)
	}
	e.ignoredIncidentCount++
	return fmt.Sprintf("Ignoring automatic snapshot pattern from %s for this server session", id)
}
