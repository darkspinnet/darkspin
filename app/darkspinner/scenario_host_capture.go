//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/darkspinnet/darkspin/server/gameplay"
	serverruntime "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/scenario/desktop"
	"github.com/darkspinnet/darkspin/server/snapshot"
)

// Capture preserves diagnostics at an unsupported boundary. Neither recording
// a bundle nor producing a comparison establishes successful client entry.
func (e *scenarioHost) Capture(ctx context.Context, kind scenario.StepKind) (scenario.Observation, error) {
	var exitArtifacts []scenario.Artifact
	var isClientExited bool
	if kind == scenario.TerminalFailure {
		exitObservation, isExited, err := e.observeClientLiveness(ctx, kind)
		if err != nil {
			exitObservation.Outcome = scenario.Inconclusive
			return exitObservation, fmt.Errorf("terminalLiveness: %w", err)
		}
		exitArtifacts = exitObservation.Artifacts
		isClientExited = isExited
	}
	result, err := e.server.ScenarioCapture(ctx, e.userID, e.authenticatedSessionID, kind)
	result.Observation.Artifacts = append(result.Observation.Artifacts, exitArtifacts...)
	for _, evidence := range result.Dump.Evidences {
		result.Observation.Artifacts = append(result.Observation.Artifacts, scenario.Artifact{
			Role: "snapshot_raw", Path: evidence.Path, SHA256: evidence.SHA256})
	}
	if isClientExited {
		result.IsInitialAuthenticationRevalidated = false
		if result.FixtureFailure != "" {
			result.FixtureFailure += "; "
		}
		result.FixtureFailure += "owned client exited; any retained fixture proof is historical without live client correlation"
		result.Observation.SessionID = ""
		result.Observation.ZoneGeneration = 0
		result.Observation.Effective = nil
	}
	if err != nil {
		return result.Observation, fmt.Errorf("captureRuntime: %w", err)
	}
	e.observationID++
	ordinal := e.observationID
	rawArtifact, err := e.persistCaptureProof(result, kind, ordinal)
	if err != nil {
		return result.Observation, fmt.Errorf("captureProof: %w", err)
	}
	observation := result.Observation
	observation.Artifacts = append(observation.Artifacts, rawArtifact)
	capture, err := e.scenarioCapture(result, rawArtifact)
	if err != nil {
		return observation, fmt.Errorf("captureAdapt: %w", err)
	}
	payload, err := json.MarshalIndent(capture, "", "  ")
	if err != nil {
		return observation, fmt.Errorf("captureMarshal: %w", err)
	}
	payload = append(payload, '\n')
	captureArtifact, err := writeScenarioHostArtifact(e.reportDirectory,
		fmt.Sprintf("capture-%04d.json", ordinal), "scenario_capture", payload)
	if err != nil {
		return observation, fmt.Errorf("captureWrite: %w", err)
	}
	observation.Artifacts = append(observation.Artifacts, captureArtifact)
	comparison, err := snapshot.CompareScenario(ctx, snapshot.ScenarioCompareRequest{Capture: capture})
	if err != nil {
		return observation, fmt.Errorf("captureCompare: %w", err)
	}
	comparison.SourceCapture = scenarioArtifactEvidence(captureArtifact, int64(len(payload)))
	reportDirectory := filepath.Join(e.reportDirectory, fmt.Sprintf("capture-%04d", ordinal))
	err = os.Mkdir(reportDirectory, 0700)
	if err != nil {
		return observation, fmt.Errorf("captureReportAllocate: %w", err)
	}
	reportPaths, reportErr := snapshot.WriteScenarioReport(ctx, snapshot.ScenarioReportRequest{Directory: reportDirectory, Comparison: comparison})
	// Finalization may retain a completed JSON when the report context expired
	// before Markdown. This small child bounds hashing that already written file;
	// it does not resume collection, gameplay or report generation.
	finalizeContext, finalizeCancel := context.WithTimeout(context.WithoutCancel(ctx), 200*time.Millisecond)
	defer finalizeCancel()
	for _, path := range []string{reportPaths.JSONPath, reportPaths.MarkdownPath} {
		if path == "" {
			continue
		}
		artifact, hashErr := desktop.HashArtifact(finalizeContext, "scenario_comparison", path)
		if hashErr != nil {
			return observation, fmt.Errorf("captureReportHash: %w", errors.Join(hashErr, reportErr))
		}
		observation.Artifacts = append(observation.Artifacts, artifact)
	}
	if reportErr != nil {
		return observation, fmt.Errorf("captureReport: %w", reportErr)
	}
	observation.Outcome = scenario.Inconclusive
	observation.Detail = "partial capture and comparison retained; native baseline, client entry and complete fixture parity remain unverified"
	persisted, err := e.persistObservation(kind, observation)
	if err != nil {
		return observation, fmt.Errorf("capturePersist: %w", err)
	}
	return persisted, nil
}

func (e *scenarioHost) persistCaptureProof(result serverruntime.ScenarioCaptureResult, kind scenario.StepKind, ordinal uint64) (scenario.Artifact, error) {
	// Fixture is the producer's explicit diagnostic DTO; no account, credentials
	// or feature aggregate crosses this file boundary.
	response := struct {
		SchemaVersion                      uint32                            `json:"schema_version"`
		RunID                              string                            `json:"run_id"`
		Boundary                           scenario.StepKind                 `json:"boundary"`
		AuthenticatedSessionID             string                            `json:"authenticated_session_id,omitempty"`
		IsInitialAuthenticationRevalidated bool                              `json:"is_initial_authentication_revalidated"`
		Fixture                            *gameplay.ScenarioFixtureEvidence `json:"fixture,omitempty"`
		FixtureFailure                     string                            `json:"fixture_failure,omitempty"`
		DumpFailure                        string                            `json:"dump_failure,omitempty"`
		DumpRequest                        string                            `json:"dump_request,omitempty"`
		DumpEvidences                      []snapshot.ScenarioEvidence       `json:"dump_evidences"`
	}{scenario.SchemaVersion, e.runID, kind, result.Observation.SessionID,
		result.IsInitialAuthenticationRevalidated, result.Fixture, result.FixtureFailure,
		result.DumpFailure, result.Dump.Request, result.Dump.Evidences}
	payload, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("proofMarshal: %w", err)
	}
	payload = append(payload, '\n')
	artifact, err := writeScenarioHostArtifact(e.reportDirectory,
		fmt.Sprintf("fixture-%04d.json", ordinal), "fixture_proof", payload)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("proofWrite: %w", err)
	}
	return artifact, nil
}

func (e *scenarioHost) scenarioCapture(result serverruntime.ScenarioCaptureResult, rawArtifact scenario.Artifact) (snapshot.ScenarioCapture, error) {
	capture := snapshot.ScenarioCapture{SchemaVersion: snapshot.ScenarioCaptureVersion,
		RunID: e.runID, ScenarioID: e.definition.ScenarioID,
		Boundaries: []snapshot.ScenarioBoundary{}, PolicyLimitations: []string{
			"Ordinary automated map-room entry is unavailable; supplied profile prerequisites are not observed traversal.",
			"Native materialization and pre-takeover mutation ordering are not established by a requested late keyframe.",
			"Retained fixture admission/commit does not establish actual transport publication or client visibility.",
			"Population scatter uses a separate uncontrolled process-global random stream.",
		}}
	rawEvidence := scenarioArtifactEvidence(rawArtifact, -1)
	fi, err := os.Stat(rawArtifact.Path)
	if err != nil {
		return snapshot.ScenarioCapture{}, fmt.Errorf("proofStat: %w", err)
	}
	size := fi.Size()
	rawEvidence.Size = &size
	var sessionGeneration, zoneGeneration *uint64
	if result.Fixture != nil && result.IsInitialAuthenticationRevalidated {
		sessionGeneration = &result.Fixture.PeerGeneration
		if result.Fixture.ZoneGeneration != 0 {
			zoneGeneration = &result.Fixture.ZoneGeneration
		}
		for _, boundary := range result.Fixture.Boundaries {
			boundary.Evidence = append(boundary.Evidence, rawEvidence)
			for index := range boundary.Objects {
				boundary.Objects[index].Evidence = append(boundary.Objects[index].Evidence, rawEvidence)
			}
			capture.Boundaries = append(capture.Boundaries, boundary)
		}
	} else {
		capture.PolicyLimitations = append(capture.PolicyLimitations, "Correlated selected fixture evidence unavailable: "+result.FixtureFailure)
	}
	if result.DumpFailure != "" {
		capture.PolicyLimitations = append(capture.PolicyLimitations, "Diagnostic snapshot incomplete: "+result.DumpFailure)
	}
	if result.Dump.ClientEvidence.Path == "" || result.Dump.Request == "" {
		for _, name := range []string{"native_layout", "fixture_takeover"} {
			capture.Boundaries = append(capture.Boundaries, snapshot.ScenarioBoundary{
				Name: name, Stage: snapshot.ScenarioClientState,
				SessionGeneration: sessionGeneration, ZoneGeneration: zoneGeneration,
				Completeness: "unavailable", Reason: "exact-request client memory response unavailable",
				Objects: []snapshot.ScenarioObject{}, Evidence: []snapshot.ScenarioEvidence{rawEvidence}})
		}
		return capture, nil
	}
	for _, name := range []string{"native_layout", "fixture_takeover"} {
		boundary, err := snapshot.CaptureScenarioClient(snapshot.ScenarioClientCaptureRequest{
			Name: name, Request: result.Dump.Request, SessionGeneration: sessionGeneration,
			ZoneGeneration: zoneGeneration, Lines: result.Dump.ClientLines, Evidence: result.Dump.ClientEvidence})
		if err != nil {
			return snapshot.ScenarioCapture{}, fmt.Errorf("clientAdapt[%s]: %w", name, err)
		}
		if !result.IsInitialAuthenticationRevalidated && boundary.Completeness != "unavailable" {
			boundary.Completeness = "unavailable"
			boundary.Reason = "client frame cannot be correlated to a revalidated isolated gameplay binding"
		}
		capture.Boundaries = append(capture.Boundaries, boundary)
	}
	return capture, nil
}

func scenarioArtifactEvidence(artifact scenario.Artifact, size int64) snapshot.ScenarioEvidence {
	evidence := snapshot.ScenarioEvidence{Path: artifact.Path, SHA256: artifact.SHA256}
	if size >= 0 {
		evidence.Size = &size
	}
	return evidence
}

func (e *scenarioHost) configureCapture(ctx context.Context, req desktop.StartRequest) error {
	if e.server == nil || e.runID != req.RunID {
		return errors.New("scenario capture configuration requires the owned server")
	}
	err := e.server.ScenarioConfigureCapture(ctx, snapshot.ScenarioRecorderRequest{
		RunID: req.RunID, ReportDirectory: req.Paths.ReportDirectory, TraceDirectory: req.Paths.TraceDirectory})
	if err != nil {
		return fmt.Errorf("hostCaptureConfigure: %w", err)
	}
	return nil
}
