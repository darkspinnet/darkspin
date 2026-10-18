//go:build scenario

package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

const ScenarioCaptureVersion uint32 = 1

type ScenarioStage string

const (
	ScenarioSelectedPlan ScenarioStage = "selected_plan"
	ScenarioServerState  ScenarioStage = "server"
	ScenarioClientState  ScenarioStage = "client"
)

// ScenarioCapture references immutable source evidence. It is an allowlisted
// comparison DTO, not a serialization of an account or gameplay aggregate.
type ScenarioCapture struct {
	SchemaVersion     uint32             `json:"schema_version"`
	RunID             string             `json:"run_id"`
	ScenarioID        string             `json:"scenario_id"`
	Boundaries        []ScenarioBoundary `json:"boundaries"`
	PolicyLimitations []string           `json:"policy_limitations"`
}

// ScenarioBoundary is a correlated interval, never an assumed atomic sample.
// Completeness is complete, partial or unavailable. Null means unknown.
type ScenarioBoundary struct {
	Name                  string             `json:"name"`
	Stage                 ScenarioStage      `json:"stage"`
	SessionGeneration     *uint64            `json:"session_generation"`
	ZoneGeneration        *uint64            `json:"zone_generation"`
	StartedAt             *time.Time         `json:"started_at"`
	CompletedAt           *time.Time         `json:"completed_at"`
	ClientStartedTimeMS   *uint64            `json:"client_started_time_ms"`
	ClientCompletedTimeMS *uint64            `json:"client_completed_time_ms"`
	StartFrame            *uint64            `json:"start_frame"`
	EndFrame              *uint64            `json:"end_frame"`
	Completeness          string             `json:"completeness"`
	Reason                string             `json:"reason,omitempty"`
	DroppedRecordCount    *uint64            `json:"dropped_record_count"`
	MalformedRecordCount  uint64             `json:"malformed_record_count"`
	IsBeforeMutation      *bool              `json:"is_before_mutation"`
	IsMutationObserved    *bool              `json:"is_mutation_observed"`
	Objects               []ScenarioObject   `json:"objects"`
	Evidence              []ScenarioEvidence `json:"evidence"`
}

type ScenarioEvidence struct {
	Path   string `json:"path"`
	Line   int    `json:"line,omitempty"`
	Size   *int64 `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

// ScenarioObject retains unmapped native handles. A missing mapping cannot
// turn an object into scenery or establish an authored marker identity.
type ScenarioObject struct {
	Kind                   string             `json:"kind,omitempty"`
	NetworkObjectID        *uint32            `json:"network_object_id"`
	ClientHandle           *uint32            `json:"client_handle"`
	ClientHandleGeneration *uint64            `json:"client_handle_generation"`
	LifecycleGeneration    *uint64            `json:"lifecycle_generation"`
	LifecycleState         string             `json:"lifecycle_state"`
	MappingProvenance      string             `json:"mapping_provenance"`
	NounName               *string            `json:"noun_name"`
	NounID                 *uint32            `json:"noun_id"`
	MarkerID               *uint32            `json:"marker_id"`
	SetName                *string            `json:"set_name"`
	SetOrdinal             *uint32            `json:"set_ordinal"`
	Position               *[3]float32        `json:"position"`
	Rotation               *[3]float32        `json:"rotation"`
	Scale                  *float32           `json:"scale"`
	IsVisible              *bool              `json:"is_visible"`
	IsCollisionEnabled     *bool              `json:"is_collision_enabled"`
	ObservedAt             *time.Time         `json:"observed_at"`
	ClientTimeMS           *uint64            `json:"client_time_ms"`
	Frame                  *uint64            `json:"frame"`
	Evidence               []ScenarioEvidence `json:"evidence"`
}

type ScenarioCompareRequest struct {
	Capture           ScenarioCapture
	PositionTolerance float64
	RotationTolerance float64
	ScaleTolerance    float64
	MaximumInterval   time.Duration
}

// ScenarioStateCaptureRequest selects exactly one session from an existing
// authoritative StateFrame; it does not collect network traffic.
type ScenarioStateCaptureRequest struct {
	Name              string
	SessionIndex      int
	SessionGeneration *uint64
	ZoneGeneration    *uint64
	State             StateFrame
	Evidence          []ScenarioEvidence
}

// ScenarioClientCaptureRequest adapts the existing client-memory.jsonl ring.
// Request identifies an exact snapshot request, never a nearest-time guess.
type ScenarioClientCaptureRequest struct {
	Name              string
	Request           string
	SessionGeneration *uint64
	ZoneGeneration    *uint64
	Lines             []string
	Evidence          ScenarioEvidence
}

// CaptureScenarioState adapts the existing allowlisted gameplay keyframe.
// Existing keyframes lack fixture marker identity and object-lifetime history;
// they cannot establish a complete fixture takeover or publication ledger.
func CaptureScenarioState(req ScenarioStateCaptureRequest) (ScenarioBoundary, error) {
	if req.Name == "" || req.SessionIndex < 0 || req.SessionIndex >= len(req.State.Sessions) {
		return ScenarioBoundary{}, errors.New("scenario state boundary or session missing")
	}
	if req.State.CapturedAt.IsZero() {
		return ScenarioBoundary{}, errors.New("scenario state capture time missing")
	}
	session := req.State.Sessions[req.SessionIndex]
	if req.SessionGeneration != nil && *req.SessionGeneration != session.SessionGeneration {
		return ScenarioBoundary{}, errors.New("scenario state session generation mismatch")
	}
	if req.ZoneGeneration != nil && *req.ZoneGeneration != session.ZoneGeneration {
		return ScenarioBoundary{}, errors.New("scenario state zone generation mismatch")
	}
	boundary := ScenarioBoundary{
		Name: req.Name, Stage: ScenarioServerState,
		// StateFrame timestamps the request before acquiring the gameplay lock;
		// it does not contain the end of collection or per-object sample times.
		StartedAt:    &req.State.CapturedAt,
		Completeness: "partial",
		Reason:       "existing StateFrame does not establish fixture identity or object lifecycle completeness",
		Evidence:     append([]ScenarioEvidence(nil), req.Evidence...),
		Objects:      make([]ScenarioObject, 0, len(session.Objects)),
	}
	if session.SessionGeneration != 0 {
		boundary.SessionGeneration = &session.SessionGeneration
	}
	if session.ZoneGeneration != 0 {
		boundary.ZoneGeneration = &session.ZoneGeneration
	}
	for _, current := range session.Objects {
		object := ScenarioObject{
			Kind: current.Kind, Position: &current.Position,
			LifecycleState: "admitted", MappingProvenance: "authoritative_state",
			Evidence: append([]ScenarioEvidence(nil), req.Evidence...),
		}
		if current.ObjectID != 0 {
			object.NetworkObjectID = &current.ObjectID
		}
		if current.NounName != "" {
			object.NounName = &current.NounName
		}
		if current.Kind == "npc" {
			object.Rotation = &current.Rotation
		}
		if current.IsPublished {
			object.LifecycleState = "published"
		}
		// IsNavigationCollisionEnabled is not general collision or visibility.
		boundary.Objects = append(boundary.Objects, object)
	}
	sort.SliceStable(boundary.Objects, func(left, right int) bool {
		return scenarioNetworkID(boundary.Objects[left]) < scenarioNetworkID(boundary.Objects[right])
	})
	return boundary, nil
}

type scenarioRegistryObservation struct {
	Status                      string  `json:"registry_status"`
	StartedTimeMS               uint64  `json:"registry_started_time_ms"`
	CompletedTimeMS             uint64  `json:"registry_completed_time_ms"`
	ObjectCount                 uint64  `json:"object_count"`
	UnreadableSlotCount         uint64  `json:"unreadable_slot_count"`
	ChangedHandleCount          uint64  `json:"changed_handle_count"`
	StartedObjectMessageCount   *uint64 `json:"started_object_message_count"`
	CompletedObjectMessageCount *uint64 `json:"completed_object_message_count"`
}

// CaptureScenarioNative reports retained earlier ordering diagnostics. The
// current hooks do not prove complete materialization or first mutation, so
// this adapter never substitutes the requested late keyframe as a baseline.
func CaptureScenarioNative(req ScenarioClientCaptureRequest) (ScenarioBoundary, error) {
	if req.Name == "" || req.Request == "" || req.Evidence.Path == "" {
		return ScenarioBoundary{}, errors.New("native boundary, request or evidence missing")
	}
	boundary := ScenarioBoundary{
		Name: req.Name, Stage: ScenarioClientState,
		SessionGeneration: req.SessionGeneration, ZoneGeneration: req.ZoneGeneration,
		Completeness: "unavailable", Objects: []ScenarioObject{},
		Evidence: []ScenarioEvidence{req.Evidence},
		Reason:   "no exact snapshot request dump; native baseline is unavailable",
	}
	clientLines := make([][]byte, len(req.Lines))
	for index, line := range req.Lines {
		clientLines[index] = []byte(line)
	}
	records, malformedCount := decodeClientTraceEvents(clientLines)
	boundary.MalformedRecordCount = uint64(malformedCount)
	clientBoundary, boundaryAt, isFound := clientReplayBoundary(records, req.Request, time.Time{})
	if !isFound {
		return boundary, nil
	}
	boundaryLine := 0
	for _, record := range records {
		if record.Event.Kind == "snapshot_boundary" && record.Event.Request == req.Request {
			boundaryLine = record.Line
		}
	}
	droppedCount := clientBoundary.CapacityDroppedLineCount
	boundary.DroppedRecordCount = &droppedCount
	retainedSceneReturnCount := 0
	retainedConstructionCount := 0
	for _, record := range records {
		if record.Line >= boundaryLine {
			break
		}
		switch record.Event.Kind {
		case "scenario_scene_load_return":
			retainedSceneReturnCount++
		case "scenario_object_message_construct":
			retainedConstructionCount++
		default:
			continue
		}
		// Source lines preserve observed ordering without assigning marker or
		// lifecycle identity. The whole immutable dump remains referenced.
		if len(boundary.Evidence) < 17 {
			evidence := req.Evidence
			evidence.Line = record.Line
			boundary.Evidence = append(boundary.Evidence, evidence)
		}
	}
	boundary.Reason = fmt.Sprintf(
		"retained ordering diagnostics: %d scene-load returns, %d object-message constructions; scene return does not prove materialization and construction does not prove dispatch or first takeover mutation; ring reset, five-minute retention, capacity eviction and allocation loss can remove earlier evidence",
		retainedSceneReturnCount, retainedConstructionCount,
	)
	// The requested late keyframe only anchors the dump. Its time/frame/object
	// sample is deliberately absent from this unavailable earlier boundary.
	if !boundaryAt.IsZero() && clientBoundary.ClientReceivedTimeMS != 0 && boundaryLine > 0 {
		evidence := req.Evidence
		evidence.Line = boundaryLine
		boundary.Evidence = append(boundary.Evidence, evidence)
	}
	return boundary, nil
}

// CaptureScenarioClient reuses the ring decoder and mapping timeline. It never
// substitutes a later snapshot for native materialization or pre-takeover.
// Those boundaries require an independent producer proving ordering.
func CaptureScenarioClient(req ScenarioClientCaptureRequest) (ScenarioBoundary, error) {
	if req.Name == "" || req.Request == "" || req.Evidence.Path == "" {
		return ScenarioBoundary{}, errors.New("scenario client boundary, request or evidence missing")
	}
	boundary := ScenarioBoundary{
		Name: req.Name, Stage: ScenarioClientState,
		SessionGeneration: req.SessionGeneration, ZoneGeneration: req.ZoneGeneration,
		Completeness: "unavailable", Reason: "matching client boundary missing",
		Evidence: []ScenarioEvidence{req.Evidence}, Objects: []ScenarioObject{},
	}
	if req.Name == "native_layout" || req.Name == "native_layout_materialization" ||
		req.Name == "pre_takeover" {
		return CaptureScenarioNative(req)
	}
	clientLines := make([][]byte, len(req.Lines))
	for index, line := range req.Lines {
		clientLines[index] = []byte(line)
	}
	records, malformedCount := decodeClientTraceEvents(clientLines)
	boundary.MalformedRecordCount = uint64(malformedCount)
	clientBoundary, boundaryAt, isFound := clientReplayBoundary(records, req.Request, time.Time{})
	if !isFound {
		return boundary, nil
	}
	droppedCount := clientBoundary.CapacityDroppedLineCount
	boundary.DroppedRecordCount = &droppedCount
	if clientBoundary.KeyframeStatus != "complete" {
		boundary.Reason = "client keyframe " + clientBoundary.KeyframeStatus
		return boundary, nil
	}
	registry, err := scenarioRegistryForBoundary(records, clientLines, clientBoundary, req.Request)
	if err != nil {
		boundary.Completeness = "partial"
		boundary.Reason = err.Error()
		return boundary, nil
	}
	boundary.ClientStartedTimeMS = &registry.StartedTimeMS
	boundary.ClientCompletedTimeMS = &registry.CompletedTimeMS
	boundary.Completeness = "partial"
	boundary.Reason = "registry interval observed; materialization, takeover and full lifecycle completeness remain unknown"
	if registry.Status != "observed" {
		boundary.Completeness = "unavailable"
		boundary.Reason = "client registry " + registry.Status
		return boundary, nil
	}
	if registry.ChangedHandleCount > 0 {
		isMutationObserved := true
		boundary.IsMutationObserved = &isMutationObserved
	}
	if !boundaryAt.IsZero() && clientBoundary.ClientReceivedTimeMS != 0 {
		startedAt := boundaryAt.Add(time.Duration(clientTimeDelta(registry.StartedTimeMS, clientBoundary.ClientReceivedTimeMS)) * time.Millisecond)
		completedAt := boundaryAt.Add(time.Duration(clientTimeDelta(registry.CompletedTimeMS, clientBoundary.ClientReceivedTimeMS)) * time.Millisecond)
		boundary.StartedAt = &startedAt
		boundary.CompletedAt = &completedAt
	}
	timeline := buildReplayTimeline(nil, clientLines, req.Evidence.Path, req.Request, boundaryAt)
	for _, event := range timeline.Events {
		if event.Kind != "object_memory" || event.ClientTimeMS < registry.StartedTimeMS ||
			event.ClientTimeMS > registry.CompletedTimeMS || event.ClientObjectID == 0 {
			continue
		}
		generation := uint64(event.ClientObjectID >> 16)
		object := ScenarioObject{
			ClientHandle: &event.ClientObjectID, ClientHandleGeneration: &generation,
			LifecycleState: "observed", MappingProvenance: event.MappingProvenance,
			ClientTimeMS: &event.ClientTimeMS, Frame: &event.FrameSequence,
		}
		if event.ObjectID != 0 {
			object.NetworkObjectID = &event.ObjectID
		} else {
			object.MappingProvenance = "unmapped_client_registry"
		}
		if len(event.PositionBits) == 3 {
			position := [3]float32{math.Float32frombits(event.PositionBits[0]), math.Float32frombits(event.PositionBits[1]), math.Float32frombits(event.PositionBits[2])}
			if scenarioFinitePosition(position) {
				object.Position = &position
			}
		}
		evidence := req.Evidence
		evidence.Line = event.ClientLine
		object.Evidence = []ScenarioEvidence{evidence}
		boundary.Objects = append(boundary.Objects, object)
		if boundary.StartFrame == nil || event.FrameSequence < *boundary.StartFrame {
			frame := event.FrameSequence
			boundary.StartFrame = &frame
		}
		if boundary.EndFrame == nil || event.FrameSequence > *boundary.EndFrame {
			frame := event.FrameSequence
			boundary.EndFrame = &frame
		}
	}
	if uint64(len(boundary.Objects)) != registry.ObjectCount || registry.UnreadableSlotCount > 0 ||
		registry.ChangedHandleCount > 0 || malformedCount > 0 || droppedCount > 0 {
		boundary.Reason = "incomplete or mutating client registry interval"
	}
	if registry.StartedObjectMessageCount != nil && registry.CompletedObjectMessageCount != nil &&
		*registry.StartedObjectMessageCount != *registry.CompletedObjectMessageCount {
		boundary.Reason = "object-message count changed between registry start and metadata emission; dispatch and mutation ordering remain unknown"
	}
	if req.Name == "fixture_takeover" || req.Name == "server_fixture_takeover" {
		boundary.Reason = "client state sampled for post-takeover comparison; " + boundary.Reason +
			"; first-mutation ordering and lifecycle completeness are unproven; this sample is not a native baseline"
	}
	sort.SliceStable(boundary.Objects, func(left, right int) bool {
		return *boundary.Objects[left].ClientHandle < *boundary.Objects[right].ClientHandle
	})
	return boundary, nil
}

func scenarioRegistryForBoundary(records []clientTraceRecord, clientLines [][]byte,
	boundary clientTraceEvent, request string) (scenarioRegistryObservation, error) {
	boundaryLine := 0
	for _, record := range records {
		if record.Event.Kind == "snapshot_boundary" && record.Event.Request == request {
			boundaryLine = record.Line
		}
	}
	if boundary.KeyframeStartedTimeMS == 0 || boundary.KeyframeCompletedTimeMS == 0 ||
		boundary.KeyframeStartedTimeMS > boundary.KeyframeCompletedTimeMS {
		return scenarioRegistryObservation{}, errors.New("client keyframe interval unavailable or tick counter wrapped")
	}
	for index := len(records) - 1; index >= 0; index-- {
		record := records[index]
		if record.Line >= boundaryLine || record.Event.Kind != "scenario_registry" {
			continue
		}
		registry := scenarioRegistryObservation{}
		err := json.Unmarshal(clientLines[record.Line-1], &registry)
		if err != nil {
			return scenarioRegistryObservation{}, fmt.Errorf("registryDecode: %w", err)
		}
		if registry.StartedTimeMS == 0 || registry.CompletedTimeMS < registry.StartedTimeMS ||
			uint64(uint32(registry.StartedTimeMS)) < boundary.KeyframeStartedTimeMS ||
			uint64(uint32(registry.CompletedTimeMS)) > boundary.KeyframeCompletedTimeMS {
			continue
		}
		return registry, nil
	}
	return scenarioRegistryObservation{}, errors.New("scenario registry interval metadata missing")
}

func scenarioFinitePosition(position [3]float32) bool {
	for _, coordinate := range position {
		if math.IsNaN(float64(coordinate)) || math.IsInf(float64(coordinate), 0) {
			return false
		}
	}
	return true
}

func scenarioNetworkID(object ScenarioObject) uint32 {
	if object.NetworkObjectID == nil {
		return 0
	}
	return *object.NetworkObjectID
}
