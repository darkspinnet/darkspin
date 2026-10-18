//go:build scenario

package snapshot

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

// ScenarioTolerances describe captured component units. Rotation units depend
// on the producer; no angle wrapping or conversion is assumed. MaximumInterval
// bounds the union of two correlated wall-clock intervals, not atomicity.
type ScenarioTolerances struct {
	Position          float64 `json:"position"`
	Rotation          float64 `json:"rotation"`
	Scale             float64 `json:"scale"`
	MaximumIntervalMS float64 `json:"maximum_interval_ms"`
}

type ScenarioComparison struct {
	SchemaVersion      uint32             `json:"schema_version"`
	RunID              string             `json:"run_id"`
	ScenarioID         string             `json:"scenario_id"`
	SourceCapture      ScenarioEvidence   `json:"source_capture"`
	RawEvidence        []ScenarioEvidence `json:"raw_evidence"`
	Outcome            string             `json:"outcome"`
	Tolerances         ScenarioTolerances `json:"tolerances"`
	LayoutParity       ScenarioParity     `json:"layout_parity"`
	ReplicationParity  ScenarioParity     `json:"replication_parity"`
	PolicyLimitations  []string           `json:"policy_limitations"`
	EarliestDivergence *ScenarioFinding   `json:"earliest_divergence"`
}

type ScenarioParity struct {
	Outcome             string            `json:"outcome"`
	BoundaryCount       int               `json:"boundary_count"`
	ExpectedObjectCount int               `json:"expected_object_count"`
	ObservedObjectCount int               `json:"observed_object_count"`
	MatchedObjectCount  int               `json:"matched_object_count"`
	Findings            []ScenarioFinding `json:"findings"`
}

// A failed finding identifies a supported discrepancy. An inconclusive finding
// records an evidence gap or spatial candidate, never a confirmed duplicate.
type ScenarioFinding struct {
	Parity     string             `json:"parity"`
	Boundary   string             `json:"boundary"`
	Kind       string             `json:"kind"`
	Outcome    string             `json:"outcome"`
	Confidence string             `json:"confidence"`
	Identity   string             `json:"identity"`
	Detail     string             `json:"detail"`
	Expected   *ScenarioObject    `json:"expected"`
	Observed   *ScenarioObject    `json:"observed"`
	OccurredAt *time.Time         `json:"occurred_at"`
	Evidence   []ScenarioEvidence `json:"evidence"`
}

type scenarioPair struct {
	ctx             context.Context
	name            string
	parity          string
	expected        ScenarioBoundary
	observed        ScenarioBoundary
	maximumInterval time.Duration
	tolerances      ScenarioTolerances
	result          *ScenarioParity
	isEvidenceValid bool
	isWindowValid   bool
	isOrderingValid bool
	digestsByPath   map[string]scenarioEvidenceDigest
}

type scenarioEvidenceDigest struct {
	size   int64
	digest string
	err    error
}

// CompareScenario only reads allowlisted capture DTOs and raw evidence. It does
// not replay gameplay or rewrite a bundle. Defaults are 0.05 world units,
// 0.01 rotation component units, 0.001 scale and a 250ms correlated interval.
// Missing/incomplete evidence forces an inconclusive parity, even when a
// supported discrepancy is separately retained in its findings.
func CompareScenario(ctx context.Context, req ScenarioCompareRequest) (ScenarioComparison, error) {
	err := ctx.Err()
	if err != nil {
		return ScenarioComparison{}, fmt.Errorf("compareContext: %w", err)
	}
	if req.Capture.SchemaVersion != ScenarioCaptureVersion {
		return ScenarioComparison{}, errors.New("unsupported scenario capture version")
	}
	if strings.TrimSpace(req.Capture.RunID) == "" || strings.TrimSpace(req.Capture.ScenarioID) == "" {
		return ScenarioComparison{}, errors.New("scenario capture run or scenario ID missing")
	}
	defaults := []float64{0.05, 0.01, 0.001}
	numbers := []float64{req.PositionTolerance, req.RotationTolerance, req.ScaleTolerance}
	for index, number := range numbers {
		if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
			return ScenarioComparison{}, errors.New("scenario tolerance must be finite and nonnegative")
		}
		if number == 0 {
			numbers[index] = defaults[index]
		}
	}
	if req.MaximumInterval < 0 {
		return ScenarioComparison{}, errors.New("scenario maximum interval must be nonnegative")
	}
	if req.MaximumInterval == 0 {
		req.MaximumInterval = 250 * time.Millisecond
	}
	result := ScenarioComparison{
		SchemaVersion: ScenarioCaptureVersion, RunID: req.Capture.RunID,
		ScenarioID: req.Capture.ScenarioID,
		Tolerances: ScenarioTolerances{Position: numbers[0], Rotation: numbers[1], Scale: numbers[2],
			MaximumIntervalMS: float64(req.MaximumInterval) / float64(time.Millisecond)},
		LayoutParity:      ScenarioParity{Findings: []ScenarioFinding{}},
		ReplicationParity: ScenarioParity{Findings: []ScenarioFinding{}},
		PolicyLimitations: append([]string(nil), req.Capture.PolicyLimitations...),
	}
	result.PolicyLimitations = append(result.PolicyLimitations,
		"Server/client enemy counts cannot establish original-server population density.",
		"Only selected authored plans are compared; excluded alternatives are not expected objects.",
		"Moving actors are sampled non-atomically; transform parity needs a simultaneous observation producer.",
		"A trigger observation does not establish encounter admission or publication.")
	boundariesByName := make(map[string][]ScenarioBoundary)
	digestsByPath := make(map[string]scenarioEvidenceDigest)
	for _, boundary := range req.Capture.Boundaries {
		if boundary.Name == "" {
			return ScenarioComparison{}, errors.New("scenario boundary name missing")
		}
		switch boundary.Stage {
		case ScenarioSelectedPlan, ScenarioServerState, ScenarioClientState:
		default:
			return ScenarioComparison{}, fmt.Errorf("boundaryStage[%s]: unknown stage %q", boundary.Name, boundary.Stage)
		}
		boundariesByName[boundary.Name] = append(boundariesByName[boundary.Name], boundary)
		result.RawEvidence = append(result.RawEvidence, boundary.Evidence...)
		for _, object := range boundary.Objects {
			result.RawEvidence = append(result.RawEvidence, object.Evidence...)
		}
	}
	names := make([]string, 0, len(boundariesByName))
	for name := range boundariesByName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, parity := range []string{"layout", "replication"} {
			expectedStage, observedStage := ScenarioSelectedPlan, ScenarioServerState
			parityResult := &result.LayoutParity
			if parity == "replication" {
				expectedStage, observedStage = ScenarioServerState, ScenarioClientState
				parityResult = &result.ReplicationParity
			}
			expecteds, observeds := scenarioBoundariesForStages(boundariesByName[name], expectedStage, observedStage)
			pair := scenarioPair{ctx: ctx, name: name, parity: parity, result: parityResult,
				maximumInterval: req.MaximumInterval, tolerances: result.Tolerances, digestsByPath: digestsByPath}
			if len(expecteds) == 0 && len(observeds) == 0 {
				continue
			}
			parityResult.BoundaryCount++
			if len(expecteds) != 1 || len(observeds) != 1 {
				pair.add("boundary_unavailable", "inconclusive", "unknown", "", nil, nil,
					fmt.Sprintf("require one %s and one %s boundary; found %d and %d", expectedStage, observedStage, len(expecteds), len(observeds)))
				continue
			}
			pair.expected, pair.observed = expecteds[0], observeds[0]
			pair.compare()
		}
	}
	scenarioFinalizeParity(&result.LayoutParity)
	scenarioFinalizeParity(&result.ReplicationParity)
	result.Outcome = scenarioCombinedOutcome(result.LayoutParity.Outcome, result.ReplicationParity.Outcome)
	findings := append([]ScenarioFinding(nil), result.LayoutParity.Findings...)
	findings = append(findings, result.ReplicationParity.Findings...)
	scenarioSortFindings(findings)
	for _, finding := range findings {
		if finding.Outcome != "failed" || finding.OccurredAt == nil || len(finding.Evidence) == 0 {
			continue
		}
		copy := finding
		result.EarliestDivergence = &copy
		break
	}
	sort.Strings(result.PolicyLimitations)
	result.PolicyLimitations = scenarioUniqueStrings(result.PolicyLimitations)
	result.RawEvidence = scenarioUniqueEvidence(result.RawEvidence)
	err = ctx.Err()
	if err != nil {
		return ScenarioComparison{}, fmt.Errorf("compareFinished: %w", err)
	}
	return result, nil
}

func scenarioBoundariesForStages(boundaries []ScenarioBoundary, expectedStage, observedStage ScenarioStage) ([]ScenarioBoundary, []ScenarioBoundary) {
	expecteds, observeds := []ScenarioBoundary{}, []ScenarioBoundary{}
	for _, boundary := range boundaries {
		if boundary.Stage == expectedStage {
			expecteds = append(expecteds, boundary)
		}
		if boundary.Stage == observedStage {
			observeds = append(observeds, boundary)
		}
	}
	return expecteds, observeds
}

func (e *scenarioPair) compare() {
	e.isOrderingValid = true
	e.result.ExpectedObjectCount += len(e.expected.Objects)
	e.result.ObservedObjectCount += len(e.observed.Objects)
	isComplete := true
	for _, boundary := range []ScenarioBoundary{e.expected, e.observed} {
		if boundary.Completeness != "complete" || boundary.DroppedRecordCount == nil ||
			*boundary.DroppedRecordCount != 0 || boundary.MalformedRecordCount != 0 ||
			boundary.IsMutationObserved == nil || *boundary.IsMutationObserved {
			isComplete = false
			e.add("capture_incomplete", "inconclusive", "unknown", "", nil, nil,
				string(boundary.Stage)+": "+boundary.Completeness+"; "+boundary.Reason)
		}
	}
	if !scenarioSameGeneration(e.expected.SessionGeneration, e.observed.SessionGeneration) ||
		!scenarioSameGeneration(e.expected.ZoneGeneration, e.observed.ZoneGeneration) {
		e.add("generation_unavailable", "inconclusive", "unknown", "", nil, nil,
			"session and zone generations must be known and equal")
		return
	}
	e.isWindowValid = e.isTimeCorrelated()
	if !e.isWindowValid {
		isComplete = false
		e.add("capture_window", "inconclusive", "unknown", "", nil, nil,
			"capture intervals are unavailable, reversed or exceed the maximum correlated interval")
	}
	if strings.Contains(e.name, "pre_takeover") || strings.Contains(e.name, "native_layout") {
		if e.observed.IsBeforeMutation == nil || !*e.observed.IsBeforeMutation {
			isComplete = false
			e.isOrderingValid = false
			e.add("ordering_unavailable", "inconclusive", "unknown", "", nil, nil,
				"native/pre-takeover boundary requires proof that observation preceded mutation")
		}
	}
	e.isEvidenceValid = e.verifyEvidence()
	if !e.isEvidenceValid {
		isComplete = false
	}
	expectedsByIdentity := e.indexObjects(e.expected.Objects, true)
	observedsByIdentity := e.indexObjects(e.observed.Objects, false)
	identities := make([]string, 0, len(expectedsByIdentity)+len(observedsByIdentity))
	for identity := range expectedsByIdentity {
		identities = append(identities, identity)
	}
	for identity := range observedsByIdentity {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	identities = scenarioUniqueStrings(identities)
	for _, identity := range identities {
		expecteds, observeds := expectedsByIdentity[identity], observedsByIdentity[identity]
		if len(expecteds) > 1 || len(observeds) > 1 {
			outcome := "inconclusive"
			confidence := "confirmed_identity"
			kind := "duplicate"
			if e.parity == "replication" && len(observeds) > 1 && !scenarioSimultaneousClientInstances(observeds) {
				kind, confidence = "repeated_observation", "unknown"
			}
			if isComplete && kind == "duplicate" {
				outcome = "failed"
			}
			e.add(kind, outcome, confidence, identity, scenarioFirstObject(expecteds), scenarioFirstObject(observeds),
				fmt.Sprintf("same confirmed identity occurs %d times in expected stage and %d times in observed stage", len(expecteds), len(observeds)))
			continue
		}
		if len(expecteds) == 0 {
			// Native statics may have no selected-plan identity; extra observed
			// objects are not automatically evidence of an all-alternative union.
			e.add("unplanned_identity", "inconclusive", "confirmed_identity", identity, nil, &observeds[0],
				"observed identity has no expected record; selection/removal history is needed")
			continue
		}
		expected := expecteds[0]
		if e.parity == "replication" && !scenarioRequiresObservation(expected.LifecycleState) {
			e.add("publication_unavailable", "inconclusive", "confirmed_identity", identity, &expected, scenarioFirstObject(observeds),
				"admitted, hidden, dormant or pending encounter records do not establish a published spawn")
			continue
		}
		if len(observeds) == 0 {
			if expected.LifecycleState == "removed" {
				e.result.MatchedObjectCount++
				continue
			}
			outcome := "inconclusive"
			if isComplete {
				outcome = "failed"
			}
			e.add("missing", outcome, "confirmed_identity", identity, &expected, nil,
				"expected identity is absent from the correlated observation; pending encounters are not inferred spawns")
			e.addSpatialCandidates(expected)
			continue
		}
		e.result.MatchedObjectCount++
		e.compareObject(identity, expected, observeds[0])
	}
}

func (e *scenarioPair) indexObjects(objects []ScenarioObject, isExpected bool) map[string][]ScenarioObject {
	objectsByIdentity := make(map[string][]ScenarioObject)
	for _, object := range objects {
		identity := e.identity(object, isExpected)
		if identity == "" {
			expected, observed := (*ScenarioObject)(nil), (*ScenarioObject)(nil)
			if isExpected {
				expected = &object
			} else {
				observed = &object
			}
			e.add("identity_unavailable", "inconclusive", "unknown", "", expected, observed,
				"identity or explicit mapping provenance unavailable; coordinates cannot establish a match")
			continue
		}
		objectsByIdentity[identity] = append(objectsByIdentity[identity], object)
	}
	return objectsByIdentity
}

func (e *scenarioPair) identity(object ScenarioObject, isExpected bool) string {
	if e.parity == "layout" {
		if object.MarkerID == nil || object.SetName == nil || *object.SetName == "" {
			return ""
		}
		isSupported := object.MappingProvenance == "selected_plan" || object.MappingProvenance == "authored_marker"
		if !isExpected {
			isSupported = object.MappingProvenance == "authoritative_marker" || object.MappingProvenance == "fixture_takeover" ||
				object.MappingProvenance == "explicit_takeover" || object.MappingProvenance == "takeover"
		}
		if !isSupported {
			return ""
		}
		ordinal := "unknown"
		if object.SetOrdinal != nil {
			ordinal = fmt.Sprint(*object.SetOrdinal)
		}
		return fmt.Sprintf("marker:%d/set:%q/ordinal:%s", *object.MarkerID, *object.SetName, ordinal)
	}
	if object.NetworkObjectID == nil || *object.NetworkObjectID == 0 || object.LifecycleGeneration == nil || *object.LifecycleGeneration == 0 {
		return ""
	}
	if isExpected {
		if object.MappingProvenance != "authoritative_state" && object.MappingProvenance != "authoritative_lifecycle" &&
			object.MappingProvenance != "fixture_takeover" {
			return ""
		}
	} else if object.ClientHandle == nil || *object.ClientHandle == 0 ||
		(object.MappingProvenance != "explicit" && object.MappingProvenance != "explicit_network_mapping") {
		return ""
	}
	return fmt.Sprintf("network:%d/lifecycle:%d", *object.NetworkObjectID, *object.LifecycleGeneration)
}

func (e *scenarioPair) compareObject(identity string, expected, observed ScenarioObject) {
	if expected.LifecycleState == "removed" {
		if observed.LifecycleState != "removed" {
			e.add("stale_deletion", "failed", "confirmed_identity", identity, &expected, &observed,
				"removed authoritative identity remains in the observed stage")
		}
		return
	}
	if observed.LifecycleState == "removed" {
		e.add("missing", "failed", "confirmed_identity", identity, &expected, &observed,
			"expected live identity is observed removed")
		return
	}
	if expected.LifecycleState == "" || observed.LifecycleState == "" ||
		(e.parity == "layout" && observed.LifecycleState != "admitted" && observed.LifecycleState != "published") ||
		(e.parity == "replication" && observed.LifecycleState != "observed" && observed.LifecycleState != "published" && observed.LifecycleState != "hidden" && observed.LifecycleState != "dormant") {
		e.add("lifecycle_unavailable", "inconclusive", "unknown", identity, &expected, &observed,
			"lifecycle state does not establish the requested stage")
	}
	if expected.NounID != nil && observed.NounID != nil {
		if *expected.NounID != *observed.NounID {
			e.add("wrong_noun", "failed", "confirmed_identity", identity, &expected, &observed, "noun IDs differ")
		}
	} else if expected.NounName != nil && observed.NounName != nil && *expected.NounName != "" && *observed.NounName != "" {
		if *expected.NounName != *observed.NounName {
			e.add("wrong_noun", "failed", "confirmed_identity", identity, &expected, &observed, "noun names differ")
		}
	} else {
		e.add("noun_unavailable", "inconclusive", "unknown", identity, &expected, &observed, "noun identity unknown")
	}
	for _, field := range []string{"visibility", "collision"} {
		expectedFlag, observedFlag := expected.IsVisible, observed.IsVisible
		if field == "collision" {
			expectedFlag, observedFlag = expected.IsCollisionEnabled, observed.IsCollisionEnabled
		}
		if expectedFlag == nil || observedFlag == nil {
			e.add(field+"_unavailable", "inconclusive", "unknown", identity, &expected, &observed, field+" unknown")
		} else if *expectedFlag != *observedFlag {
			e.add(field, "failed", "confirmed_identity", identity, &expected, &observed, field+" differs")
		}
	}
	if expected.SetOrdinal == nil || observed.SetOrdinal == nil {
		if e.parity == "layout" {
			e.add("set_ordinal_unavailable", "inconclusive", "unknown", identity, &expected, &observed, "selected set ordinal unknown")
		}
	}
	if !scenarioIsStatic(expected.Kind) || !scenarioIsStatic(observed.Kind) {
		e.add("non_atomic_transform", "inconclusive", "unknown", identity, &expected, &observed,
			"moving or unknown-kind actors cannot establish transform parity from correlated intervals")
		return
	}
	for _, field := range []string{"position", "rotation", "scale"} {
		isKnown, isEqual := scenarioTransformEqual(field, expected, observed, e.tolerances)
		if !isKnown {
			e.add("transform_unavailable", "inconclusive", "unknown", identity, &expected, &observed, field+" unknown or nonfinite")
		} else if !isEqual {
			e.add("transform", "failed", "confirmed_identity", identity, &expected, &observed, field+" exceeds declared tolerance")
		}
	}
}

func scenarioTransformEqual(field string, expected, observed ScenarioObject, tolerances ScenarioTolerances) (bool, bool) {
	if field == "scale" {
		if expected.Scale == nil || observed.Scale == nil {
			return false, false
		}
		left, right := float64(*expected.Scale), float64(*observed.Scale)
		if math.IsNaN(left) || math.IsInf(left, 0) || math.IsNaN(right) || math.IsInf(right, 0) {
			return false, false
		}
		return true, math.Abs(left-right) <= tolerances.Scale
	}
	lefts, rights, tolerance := expected.Position, observed.Position, tolerances.Position
	if field == "rotation" {
		lefts, rights, tolerance = expected.Rotation, observed.Rotation, tolerances.Rotation
	}
	if lefts == nil || rights == nil || !scenarioFinitePosition(*lefts) || !scenarioFinitePosition(*rights) {
		return false, false
	}
	distanceSquared := float64(0)
	for index, left := range lefts {
		difference := float64(left) - float64(rights[index])
		if field == "rotation" && math.Abs(difference) > tolerance {
			return true, false
		}
		distanceSquared += difference * difference
	}
	if field == "rotation" {
		return true, true
	}
	return true, distanceSquared <= tolerance*tolerance
}

func (e *scenarioPair) addSpatialCandidates(expected ScenarioObject) {
	for _, observed := range e.observed.Objects {
		if e.identity(observed, false) != "" {
			continue
		}
		isKnown, isNear := scenarioTransformEqual("position", expected, observed, e.tolerances)
		if isKnown && isNear {
			e.add("spatial_candidate", "inconclusive", "candidate", "", &expected, &observed,
				"positions are close; this supplies neither identity nor duplicate proof")
		}
	}
}

func (e *scenarioPair) isTimeCorrelated() bool {
	left, right := e.expected, e.observed
	if left.StartedAt == nil || left.CompletedAt == nil || right.StartedAt == nil || right.CompletedAt == nil {
		return false
	}
	if left.StartedAt.IsZero() || left.CompletedAt.IsZero() || right.StartedAt.IsZero() || right.CompletedAt.IsZero() {
		return false
	}
	if left.CompletedAt.Before(*left.StartedAt) || right.CompletedAt.Before(*right.StartedAt) {
		return false
	}
	for _, boundary := range []ScenarioBoundary{left, right} {
		for _, object := range boundary.Objects {
			if object.ObservedAt != nil && (object.ObservedAt.IsZero() || object.ObservedAt.Before(*boundary.StartedAt) || object.ObservedAt.After(*boundary.CompletedAt)) {
				return false
			}
		}
	}
	start, end := *left.StartedAt, *left.CompletedAt
	if right.StartedAt.Before(start) {
		start = *right.StartedAt
	}
	if right.CompletedAt.After(end) {
		end = *right.CompletedAt
	}
	return end.Sub(start) <= e.maximumInterval
}

func (e *scenarioPair) verifyEvidence() bool {
	isValid := true
	evidences := append([]ScenarioEvidence(nil), e.expected.Evidence...)
	evidences = append(evidences, e.observed.Evidence...)
	for _, boundary := range []ScenarioBoundary{e.expected, e.observed} {
		if len(boundary.Evidence) == 0 {
			isValid = false
			e.add("evidence_unavailable", "inconclusive", "unknown", "", nil, nil, string(boundary.Stage)+" raw evidence missing")
		}
		for _, object := range boundary.Objects {
			evidences = append(evidences, object.Evidence...)
		}
	}
	for _, evidence := range scenarioUniqueEvidence(evidences) {
		err := e.ctx.Err()
		if err != nil {
			e.add("comparison_canceled", "inconclusive", "unknown", "", nil, nil, "comparison context ended")
			return false
		}
		if evidence.Path == "" || evidence.Size == nil || *evidence.Size < 0 || len(evidence.SHA256) != 64 {
			isValid = false
			e.add("integrity_unavailable", "inconclusive", "unknown", "", nil, nil, "raw evidence size or digest missing: "+evidence.Path)
			continue
		}
		result, isHashed := e.digestsByPath[evidence.Path]
		if !isHashed {
			fi, statErr := os.Lstat(evidence.Path)
			if statErr != nil {
				result.err = fmt.Errorf("evidenceStat: %w", statErr)
			} else if !fi.Mode().IsRegular() {
				result.err = errors.New("raw scenario evidence must be a regular file")
			} else {
				result.size, result.digest, result.err = hashFile(evidence.Path)
			}
			e.digestsByPath[evidence.Path] = result
		}
		if result.err != nil || result.size != *evidence.Size || !strings.EqualFold(result.digest, evidence.SHA256) {
			isValid = false
			e.add("integrity_unavailable", "inconclusive", "unknown", "", nil, nil, "raw evidence missing or digest changed: "+evidence.Path)
		}
	}
	return isValid
}

func (e *scenarioPair) add(kind, outcome, confidence, identity string, expected, observed *ScenarioObject, detail string) {
	if outcome == "failed" && (!e.isEvidenceValid || !e.isWindowValid || !e.isOrderingValid) {
		outcome, confidence = "inconclusive", "unknown"
		detail += "; raw evidence integrity, time correlation or mutation ordering is unavailable"
	}
	evidences := append([]ScenarioEvidence(nil), e.expected.Evidence...)
	evidences = append(evidences, e.observed.Evidence...)
	var occurredAt *time.Time
	if observed != nil {
		evidences = append(evidences, observed.Evidence...)
		occurredAt = observed.ObservedAt
	}
	if expected != nil {
		evidences = append(evidences, expected.Evidence...)
	}
	// Completion is the earliest bound at which an interval-wide absence can
	// be supported. Never use its start or the requested milestone time.
	if occurredAt == nil {
		occurredAt = e.observed.CompletedAt
	}
	e.result.Findings = append(e.result.Findings, ScenarioFinding{
		Parity: e.parity, Boundary: e.name, Kind: kind, Outcome: outcome,
		Confidence: confidence, Identity: identity, Detail: detail,
		Expected: expected, Observed: observed, OccurredAt: occurredAt,
		Evidence: scenarioUniqueEvidence(evidences),
	})
}

func scenarioSameGeneration(left, right *uint64) bool {
	return left != nil && right != nil && *left > 0 && *left == *right
}

func scenarioRequiresObservation(state string) bool {
	return state == "published" || state == "removed"
}

func scenarioIsStatic(kind string) bool {
	return kind == "fixture" || kind == "static" || kind == "scenery" || kind == "destructible"
}

func scenarioSimultaneousClientInstances(objects []ScenarioObject) bool {
	handles := make(map[uint32]bool)
	var frame *uint64
	for _, object := range objects {
		if object.ClientHandle == nil || *object.ClientHandle == 0 || object.Frame == nil || handles[*object.ClientHandle] {
			return false
		}
		if frame != nil && *frame != *object.Frame {
			return false
		}
		frame = object.Frame
		handles[*object.ClientHandle] = true
	}
	return len(handles) > 1
}

func scenarioFirstObject(objects []ScenarioObject) *ScenarioObject {
	if len(objects) == 0 {
		return nil
	}
	return &objects[0]
}

func scenarioFinalizeParity(parity *ScenarioParity) {
	scenarioSortFindings(parity.Findings)
	parity.Outcome = "passed"
	if parity.BoundaryCount == 0 || (parity.ExpectedObjectCount == 0 && parity.ObservedObjectCount == 0) {
		parity.Outcome = "inconclusive"
	}
	for _, finding := range parity.Findings {
		parity.Outcome = scenarioCombinedOutcome(parity.Outcome, finding.Outcome)
	}
}

func scenarioCombinedOutcome(left, right string) string {
	if left == "inconclusive" || right == "inconclusive" {
		return "inconclusive"
	}
	if left == "failed" || right == "failed" {
		return "failed"
	}
	return "passed"
}

func scenarioSortFindings(findings []ScenarioFinding) {
	sort.SliceStable(findings, func(left, right int) bool {
		a, b := findings[left], findings[right]
		if a.OccurredAt != nil && b.OccurredAt == nil {
			return true
		}
		if a.OccurredAt == nil && b.OccurredAt != nil {
			return false
		}
		if a.OccurredAt != nil && !a.OccurredAt.Equal(*b.OccurredAt) {
			return a.OccurredAt.Before(*b.OccurredAt)
		}
		return scenarioFindingKey(a) < scenarioFindingKey(b)
	})
}

func scenarioFindingKey(finding ScenarioFinding) string {
	// Include local handles and raw source lines for deterministic ties among
	// unmapped objects. IDs are not used as an identity inference.
	key := finding.Parity + "/" + finding.Boundary + "/" + finding.Identity + "/" + finding.Kind + "/" + finding.Detail
	for _, object := range []*ScenarioObject{finding.Expected, finding.Observed} {
		if object != nil {
			key += fmt.Sprintf("/%d", scenarioNetworkID(*object))
			if object.ClientHandle != nil {
				key += fmt.Sprintf("/%d", *object.ClientHandle)
			}
		}
	}
	for _, evidence := range finding.Evidence {
		key += fmt.Sprintf("/%s:%d", evidence.Path, evidence.Line)
	}
	return key
}

func scenarioUniqueStrings(strings []string) []string {
	uniques := make([]string, 0, len(strings))
	for _, text := range strings {
		if len(uniques) == 0 || uniques[len(uniques)-1] != text {
			uniques = append(uniques, text)
		}
	}
	return uniques
}

func scenarioUniqueEvidence(evidences []ScenarioEvidence) []ScenarioEvidence {
	copies := append([]ScenarioEvidence(nil), evidences...)
	sort.SliceStable(copies, func(left, right int) bool {
		return scenarioEvidenceKey(copies[left]) < scenarioEvidenceKey(copies[right])
	})
	uniques := make([]ScenarioEvidence, 0, len(copies))
	for _, evidence := range copies {
		if len(uniques) == 0 || scenarioEvidenceKey(uniques[len(uniques)-1]) != scenarioEvidenceKey(evidence) {
			uniques = append(uniques, evidence)
		}
	}
	return uniques
}

func scenarioEvidenceKey(evidence ScenarioEvidence) string {
	size := int64(-1)
	if evidence.Size != nil {
		size = *evidence.Size
	}
	return fmt.Sprintf("%s:%d:%d:%s", evidence.Path, evidence.Line, size, evidence.SHA256)
}
