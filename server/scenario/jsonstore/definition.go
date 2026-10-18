//go:build scenario

// Package jsonstore adapts scenario definitions and manifests to local files.
package jsonstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
)

type milestoneDTO struct {
	Kind      scenario.StepKind `json:"kind"`
	TimeoutMS *int64            `json:"timeout_ms"`
}

type definitionDTO struct {
	SchemaVersion        *uint32        `json:"schema_version"`
	ScenarioID           *string        `json:"scenario_id"`
	Level                *string        `json:"level"`
	Occurrence           *uint32        `json:"occurrence"`
	Difficulty           *uint32        `json:"difficulty"`
	MapSeed              *uint32        `json:"map_seed"`
	PrepareMask          *uint32        `json:"prepare_mask"`
	PopulationProvenance *string        `json:"population_provenance"`
	ProgressionID        *string        `json:"progression_id"`
	SquadID              *string        `json:"squad_id"`
	HeroIDs              []uint64       `json:"hero_ids"`
	ClientCount          *uint32        `json:"client_count"`
	Milestones           []milestoneDTO `json:"milestones"`
}

// ReadDefinition rejects omitted required inputs, unknown keys and trailing JSON.
// Returned provenance is a digest of exactly the bytes parsed, not later reads.
func ReadDefinition(ctx context.Context, path string) (scenario.RunRequest, error) {
	err := ctx.Err()
	if err != nil {
		return scenario.RunRequest{}, fmt.Errorf("definitionContext: %w", err)
	}
	r, err := os.Open(path)
	if err != nil {
		return scenario.RunRequest{}, fmt.Errorf("definitionOpen: %w", err)
	}
	fi, statErr := r.Stat()
	if statErr != nil || !fi.Mode().IsRegular() {
		closeErr := r.Close()
		if statErr == nil {
			statErr = errors.New("scenario definition must be a regular file")
		}
		return scenario.RunRequest{}, fmt.Errorf("definitionStat: %w", errors.Join(statErr, closeErr))
	}
	payload, readErr := io.ReadAll(io.LimitReader(r, 1024*1024+1))
	closeErr := r.Close()
	if readErr != nil {
		return scenario.RunRequest{}, fmt.Errorf("definitionRead: %w", errors.Join(readErr, closeErr))
	}
	if closeErr != nil {
		return scenario.RunRequest{}, fmt.Errorf("definitionClose: %w", closeErr)
	}
	if len(payload) > 1024*1024 {
		return scenario.RunRequest{}, errors.New("scenario definition exceeds 1 MiB")
	}
	var definition definitionDTO
	err = decodeStrict(payload, &definition)
	if err != nil {
		return scenario.RunRequest{}, fmt.Errorf("definitionDecode: %w", err)
	}
	if definition.SchemaVersion == nil || definition.ScenarioID == nil || definition.Level == nil ||
		definition.Occurrence == nil || definition.Difficulty == nil || definition.MapSeed == nil ||
		definition.PrepareMask == nil || definition.PopulationProvenance == nil ||
		definition.ProgressionID == nil || definition.SquadID == nil || definition.ClientCount == nil {
		return scenario.RunRequest{}, errors.New("scenario definition requires all explicit input fields")
	}
	req := scenario.RunRequest{Definition: scenario.Definition{
		SchemaVersion: *definition.SchemaVersion, ScenarioID: *definition.ScenarioID,
		Level: *definition.Level, Occurrence: *definition.Occurrence, Difficulty: *definition.Difficulty,
		MapSeed: *definition.MapSeed, PrepareMask: *definition.PrepareMask,
		PopulationProvenance: *definition.PopulationProvenance, ProgressionID: *definition.ProgressionID,
		SquadID: *definition.SquadID, HeroIDs: definition.HeroIDs, ClientCount: *definition.ClientCount,
	}}
	for index, milestone := range definition.Milestones {
		if milestone.TimeoutMS == nil || *milestone.TimeoutMS < 1000 || *milestone.TimeoutMS > 600000 {
			return scenario.RunRequest{}, fmt.Errorf("milestone %d requires timeout_ms between 1000 and 600000", index)
		}
		req.Definition.Milestones = append(req.Definition.Milestones, scenario.Milestone{
			Kind: milestone.Kind, Timeout: time.Duration(*milestone.TimeoutMS) * time.Millisecond,
		})
	}
	err = req.Definition.Validate()
	if err != nil {
		return scenario.RunRequest{}, fmt.Errorf("definitionValidate: %w", err)
	}
	digest := sha256.Sum256(payload)
	req.Input = scenario.Artifact{Role: "scenario_definition", Path: path, SHA256: hex.EncodeToString(digest[:])}
	return req, nil
}

// marshalDefinition explicitly allowlists persisted input fields.
func marshalDefinition(definition scenario.Definition) definitionDTO {
	dto := definitionDTO{
		SchemaVersion: &definition.SchemaVersion, ScenarioID: &definition.ScenarioID,
		Level: &definition.Level, Occurrence: &definition.Occurrence, Difficulty: &definition.Difficulty,
		MapSeed: &definition.MapSeed, PrepareMask: &definition.PrepareMask,
		PopulationProvenance: &definition.PopulationProvenance, ProgressionID: &definition.ProgressionID,
		SquadID: &definition.SquadID, HeroIDs: definition.HeroIDs, ClientCount: &definition.ClientCount,
	}
	for _, milestone := range definition.Milestones {
		timeoutMS := milestone.Timeout.Milliseconds()
		dto.Milestones = append(dto.Milestones, milestoneDTO{Kind: milestone.Kind, TimeoutMS: &timeoutMS})
	}
	return dto
}

// rejectDuplicateKeys applies recursively before normal DTO decoding because
// encoding/json otherwise silently accepts the last occurrence of each key.
func rejectDuplicateKeys(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("jsonToken: %w", err)
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	keys := map[string]struct{}{}
	for decoder.More() {
		if delimiter == '{' {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return fmt.Errorf("jsonKey: %w", keyErr)
			}
			key, isString := keyToken.(string)
			if !isString {
				return errors.New("JSON object key must be a string")
			}
			_, isDuplicate := keys[key]
			if isDuplicate {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			keys[key] = struct{}{}
		}
		err = rejectDuplicateKeys(decoder)
		if err != nil {
			return fmt.Errorf("jsonChild: %w", err)
		}
	}
	endToken, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("jsonEnd: %w", err)
	}
	end, isEnd := endToken.(json.Delim)
	if !isEnd || delimiter == '{' && end != '}' || delimiter == '[' && end != ']' {
		return errors.New("invalid JSON container ending")
	}
	return nil
}
