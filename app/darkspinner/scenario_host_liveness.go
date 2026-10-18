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

	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/scenario/desktop"
)

type scenarioClientLivenessReader interface {
	Liveness(context.Context) (scenarioClientLiveness, error)
}

// Binding occurs once in the serial worker before resume or any observations.
func (e *scenarioHost) BindClientLiveness(reader scenarioClientLivenessReader) error {
	if e == nil || reader == nil {
		return errors.New("client liveness binding requires host and owned reader")
	}
	if e.clientLivenessReader != nil {
		return errors.New("client liveness reader is already bound")
	}
	e.clientLivenessReader = reader
	return nil
}

func (e *scenarioHost) checkClientObservation(ctx context.Context, kind scenario.StepKind) (scenario.Observation, bool, error) {
	observation, isExited, err := e.observeClientLiveness(ctx, kind)
	if err != nil {
		observation.Outcome = scenario.Inconclusive
		observation.Detail = "owned client liveness observation failed: " + err.Error()
		return observation, true, fmt.Errorf("livenessObserve: %w", err)
	}
	if !isExited {
		return scenario.Observation{}, false, nil
	}
	persisted, err := e.persistObservation(kind, observation)
	if err != nil {
		observation.Outcome = scenario.Inconclusive
		return observation, true, fmt.Errorf("livenessPersist: %w", err)
	}
	err = ctx.Err()
	if err != nil {
		persisted.Outcome = scenario.Inconclusive
		persisted.Detail = "owned client exit observed at the observation deadline"
		return persisted, true, fmt.Errorf("livenessFinalContext: %w", err)
	}
	return persisted, true, nil
}

// observeClientLiveness never closes the worker or client. A positive exit
// retains raw evidence before the runner requests its separate terminal capture.
func (e *scenarioHost) observeClientLiveness(ctx context.Context, kind scenario.StepKind) (scenario.Observation, bool, error) {
	observation := scenario.Observation{Outcome: scenario.Inconclusive,
		Detail: "owned client liveness is unavailable"}
	if e.clientLivenessReader == nil {
		return observation, false, errors.New("owned client liveness reader is not bound")
	}
	proof, err := e.clientLivenessReader.Liveness(ctx)
	if err != nil {
		return observation, false, fmt.Errorf("clientLiveness: %w", err)
	}
	if proof.ProcessID == 0 || proof.ObservedAt.IsZero() || proof.IsAlive == proof.IsExited ||
		proof.IsExited && proof.ExitCode == nil || proof.IsAlive && proof.ExitCode != nil {
		return observation, false, errors.New("owned client liveness proof is incomplete")
	}
	if proof.IsAlive {
		return scenario.Observation{}, false, nil
	}
	observation.Outcome = scenario.Failed
	observation.Detail = fmt.Sprintf("owned client process %d exited with code %d before %s; renderer cause is unclassified",
		proof.ProcessID, *proof.ExitCode, kind)
	artifact, err := e.persistClientExit(kind, proof)
	if err != nil {
		observation.Outcome = scenario.Inconclusive
		return observation, true, fmt.Errorf("exitPersist: %w", err)
	}
	observation.Artifacts = append(observation.Artifacts, artifact)
	traceArtifact, err := e.completedClientTrace(ctx)
	if err != nil {
		observation.Outcome = scenario.Inconclusive
		return observation, true, fmt.Errorf("exitTrace: %w", err)
	}
	if traceArtifact.Path != "" {
		observation.Artifacts = append(observation.Artifacts, traceArtifact)
	}
	err = ctx.Err()
	if err != nil {
		observation.Outcome = scenario.Inconclusive
		return observation, true, fmt.Errorf("exitContext: %w", err)
	}
	return observation, true, nil
}

func (e *scenarioHost) persistClientExit(kind scenario.StepKind, proof scenarioClientLiveness) (scenario.Artifact, error) {
	response := struct {
		SchemaVersion uint32            `json:"schema_version"`
		RunID         string            `json:"run_id"`
		Boundary      scenario.StepKind `json:"boundary"`
		ProcessID     uint32            `json:"process_id"`
		ObservedAt    time.Time         `json:"observed_at"`
		IsAlive       bool              `json:"is_alive"`
		IsExited      bool              `json:"is_exited"`
		ExitCode      *uint32           `json:"exit_code"`
		Scope         string            `json:"scope"`
	}{scenario.SchemaVersion, e.runID, kind, proof.ProcessID, proof.ObservedAt,
		proof.IsAlive, proof.IsExited, proof.ExitCode,
		"original retained process handle; exit is observed, renderer cause and gameplay readiness are unclassified"}
	payload, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("exitMarshal: %w", err)
	}
	payload = append(payload, '\n')
	e.observationID++
	artifact, err := writeScenarioHostArtifact(e.reportDirectory,
		fmt.Sprintf("client-exit-%04d.json", e.observationID), "client_exit", payload)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("exitWrite: %w", err)
	}
	return artifact, nil
}

// Only called after the original process handle is signaled, so its trace
// writer has stopped. Never hash a live client trace as immutable evidence.
func (e *scenarioHost) completedClientTrace(ctx context.Context) (scenario.Artifact, error) {
	path := filepath.Join(e.reportDirectory, "client.jsonl")
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return scenario.Artifact{}, nil
	}
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("traceStat: %w", err)
	}
	if !fi.Mode().IsRegular() || fi.Size() > 128*1024*1024 {
		return scenario.Artifact{}, errors.New("completed client trace is not a bounded regular file")
	}
	artifact, err := desktop.HashArtifact(ctx, "client_trace", path)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("traceHash: %w", err)
	}
	currentFI, err := os.Lstat(path)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("traceRecheck: %w", err)
	}
	if !currentFI.Mode().IsRegular() || !os.SameFile(fi, currentFI) ||
		fi.Size() != currentFI.Size() || !fi.ModTime().Equal(currentFI.ModTime()) {
		return scenario.Artifact{}, errors.New("completed client trace changed during hashing")
	}
	return artifact, nil
}
