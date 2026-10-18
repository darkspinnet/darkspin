//go:build scenario

package jsonstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
)

type artifactDTO struct {
	Role   string `json:"role"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitempty"`
}

type pathsDTO struct {
	CacheDirectory  string `json:"cache_directory"`
	ReportDirectory string `json:"report_directory"`
	TraceDirectory  string `json:"trace_directory"`
	ManifestPath    string `json:"manifest_path"`
}

type effectiveDTO struct {
	Level                string   `json:"level"`
	Occurrence           uint32   `json:"occurrence"`
	Difficulty           uint32   `json:"difficulty"`
	MapSeed              uint32   `json:"map_seed"`
	PrepareMask          uint32   `json:"prepare_mask"`
	PopulationProvenance string   `json:"population_provenance"`
	UnknownRNGOwners     []string `json:"unknown_rng_owners"`
}

type capabilityDTO struct {
	ProtocolVersion uint32   `json:"protocol_version"`
	Component       string   `json:"component"`
	BuildID         string   `json:"build_id"`
	Features        []string `json:"features"`
}

type versionDTO struct {
	Component string      `json:"component"`
	BuildID   string      `json:"build_id"`
	Artifact  artifactDTO `json:"artifact"`
}

type observationDTO struct {
	Outcome        scenario.Outcome `json:"outcome"`
	Detail         string           `json:"detail"`
	SessionID      string           `json:"session_id,omitempty"`
	ZoneGeneration uint64           `json:"zone_generation,omitempty"`
	Effective      *effectiveDTO    `json:"effective,omitempty"`
	Artifacts      []artifactDTO    `json:"artifacts"`
}

type stepDTO struct {
	Kind        scenario.StepKind `json:"kind"`
	Outcome     scenario.Outcome  `json:"outcome"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	FinishedAt  *time.Time        `json:"finished_at,omitempty"`
	Detail      string            `json:"detail"`
	Observation observationDTO    `json:"observation"`
}

type manifestDTO struct {
	SchemaVersion uint32            `json:"schema_version"`
	RunID         string            `json:"run_id"`
	Definition    definitionDTO     `json:"definition"`
	Input         artifactDTO       `json:"input"`
	Paths         pathsDTO          `json:"paths"`
	StartedAt     time.Time         `json:"started_at"`
	FinishedAt    *time.Time        `json:"finished_at,omitempty"`
	Outcome       scenario.Outcome  `json:"outcome"`
	Detail        string            `json:"detail"`
	LastReached   scenario.StepKind `json:"last_reached,omitempty"`
	Capabilities  []capabilityDTO   `json:"capabilities"`
	Versions      []versionDTO      `json:"versions"`
	Effective     *effectiveDTO     `json:"effective,omitempty"`
	Steps         []stepDTO         `json:"steps"`
	Artifacts     []artifactDTO     `json:"artifacts"`
}

func marshalManifest(manifest scenario.Manifest) manifestDTO {
	dto := manifestDTO{
		SchemaVersion: manifest.SchemaVersion, RunID: manifest.RunID,
		Definition: marshalDefinition(manifest.Definition), Input: marshalArtifact(manifest.Input),
		Paths: pathsDTO{CacheDirectory: manifest.Paths.CacheDirectory,
			ReportDirectory: manifest.Paths.ReportDirectory, TraceDirectory: manifest.Paths.TraceDirectory,
			ManifestPath: manifest.Paths.ManifestPath}, StartedAt: manifest.StartedAt,
		FinishedAt: optionalTime(manifest.FinishedAt), Outcome: manifest.Outcome,
		Detail: manifest.Detail, LastReached: manifest.LastReached, Effective: marshalEffective(manifest.Effective),
		Artifacts: marshalArtifacts(manifest.Artifacts),
	}
	for _, capability := range manifest.Capabilities {
		dto.Capabilities = append(dto.Capabilities, capabilityDTO{ProtocolVersion: capability.ProtocolVersion,
			Component: capability.Component, BuildID: capability.BuildID, Features: capability.Features})
	}
	for _, version := range manifest.Versions {
		dto.Versions = append(dto.Versions, versionDTO{Component: version.Component,
			BuildID: version.BuildID, Artifact: marshalArtifact(version.Artifact)})
	}
	for _, step := range manifest.Steps {
		dto.Steps = append(dto.Steps, stepDTO{Kind: step.Kind, Outcome: step.Outcome,
			StartedAt: optionalTime(step.StartedAt), FinishedAt: optionalTime(step.FinishedAt), Detail: step.Detail,
			Observation: observationDTO{Outcome: step.Observation.Outcome, Detail: step.Observation.Detail,
				SessionID: step.Observation.SessionID, ZoneGeneration: step.Observation.ZoneGeneration,
				Effective: marshalEffective(step.Observation.Effective), Artifacts: marshalArtifacts(step.Observation.Artifacts)}})
	}
	return dto
}

func marshalEffective(effective *scenario.EffectiveInputs) *effectiveDTO {
	if effective == nil {
		return nil
	}
	return &effectiveDTO{Level: effective.Level, Occurrence: effective.Occurrence, Difficulty: effective.Difficulty,
		MapSeed: effective.MapSeed, PrepareMask: effective.PrepareMask, PopulationProvenance: effective.PopulationProvenance,
		UnknownRNGOwners: effective.UnknownRNGOwners}
}

func marshalArtifact(artifact scenario.Artifact) artifactDTO {
	return artifactDTO{Role: artifact.Role, Path: artifact.Path, SHA256: artifact.SHA256}
}

func marshalArtifacts(artifacts []scenario.Artifact) []artifactDTO {
	dtos := make([]artifactDTO, 0, len(artifacts))
	for _, artifact := range artifacts {
		dtos = append(dtos, marshalArtifact(artifact))
	}
	return dtos
}

func optionalTime(timestamp time.Time) *time.Time {
	if timestamp.IsZero() {
		return nil
	}
	return &timestamp
}

func decodeStrict(payload []byte, destination any) error {
	keyDecoder := json.NewDecoder(bytes.NewReader(payload))
	err := rejectDuplicateKeys(keyDecoder)
	if err != nil {
		return fmt.Errorf("jsonKeys: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(destination)
	if err != nil {
		return fmt.Errorf("jsonDecode: %w", err)
	}
	var trailing any
	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("jsonTrailing: %w", err)
		}
		return errors.New("scenario JSON contains more than one document")
	}
	return nil
}
