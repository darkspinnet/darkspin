//go:build scenario

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/darkspinnet/darkspin/server/gameplay"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

func (e *scenarioHost) persistObserved(
	kind scenario.StepKind, observation scenario.Observation, proof *gameplay.ScenarioPrepareEvidence,
	commitProof *gameplay.ScenarioDungeonCommitEvidence,
) (scenario.Observation, error) {
	if kind == scenario.Authenticated && observation.Outcome == scenario.Passed && observation.SessionID == "" {
		return scenario.Observation{}, errors.New("accepted authentication lacks live session identity")
	}
	if proof != nil && proof.UserID != 0 {
		artifact, err := e.persistPrepare(*proof, observation)
		if err != nil {
			return scenario.Observation{}, fmt.Errorf("preparePersist: %w", err)
		}
		observation.Artifacts = append(observation.Artifacts, artifact)
	}
	if commitProof != nil && commitProof.UserID != 0 {
		artifact, err := e.persistDungeonCommit(*commitProof, observation)
		if err != nil {
			return observation, fmt.Errorf("commitPersist: %w", err)
		}
		observation.Artifacts = append(observation.Artifacts, artifact)
	}
	persisted, err := e.persistObservation(kind, observation)
	if err != nil {
		return observation, fmt.Errorf("observedPersist: %w", err)
	}
	if kind == scenario.Authenticated && persisted.Outcome == scenario.Passed {
		e.authenticatedSessionID = persisted.SessionID
	}
	return persisted, nil
}

func (e *scenarioHost) persistPrepare(
	proof gameplay.ScenarioPrepareEvidence, observation scenario.Observation,
) (scenario.Artifact, error) {
	response := struct {
		SchemaVersion                         uint32                       `json:"schema_version"`
		Outcome                               scenario.Outcome             `json:"outcome"`
		AuthenticatedSessionID                string                       `json:"authenticated_session_id"`
		IsInitialAuthenticationRevalidated    bool                         `json:"is_initial_authentication_revalidated"`
		UserID                                uint64                       `json:"user_id"`
		GameID                                uint32                       `json:"game_id"`
		PeerGeneration                        uint64                       `json:"peer_generation"`
		TransportGeneration                   uint64                       `json:"transport_generation"`
		GameplaySessionID                     string                       `json:"gameplay_session_id"`
		PrepareQueuedAt                       time.Time                    `json:"prepare_queued_at"`
		StatusReceivedAt                      time.Time                    `json:"status_received_at"`
		AcceptedAt                            time.Time                    `json:"accepted_at"`
		Status                                uint32                       `json:"status"`
		Progress                              float32                      `json:"progress"`
		EffectiveLevelID                      uint32                       `json:"effective_level_id"`
		EffectiveMapSeed                      uint32                       `json:"effective_map_seed"`
		EffectivePrepareMask                  uint32                       `json:"effective_prepare_mask"`
		EffectiveOccurrence                   uint32                       `json:"effective_occurrence"`
		EffectiveDifficulty                   uint32                       `json:"effective_difficulty"`
		PreparePayload                        []byte                       `json:"prepare_payload"`
		StatusPayload                         []byte                       `json:"status_payload"`
		StatusMessageID                       uint8                        `json:"status_message_id"`
		StatusTraceID                         uint64                       `json:"status_trace_id"`
		StatusSourceTime                      uint64                       `json:"status_source_time"`
		IsWirePublicationObserved             bool                         `json:"is_wire_publication_observed"`
		IsClientSeedEchoObserved              bool                         `json:"is_client_seed_echo_observed"`
		IsAuthoritativeZoneGenerationObserved bool                         `json:"is_authoritative_zone_generation_observed"`
		RequestedPopulationProvenance         string                       `json:"requested_population_provenance"`
		ProfileSetupArtifact                  string                       `json:"profile_setup_artifact"`
		ProfileSetupSHA256                    string                       `json:"profile_setup_sha256"`
		Effective                             *scenarioHostEffectiveInputs `json:"effective,omitempty"`
	}{SchemaVersion: scenario.SchemaVersion, Outcome: observation.Outcome,
		AuthenticatedSessionID: e.authenticatedSessionID, UserID: proof.UserID,
		IsInitialAuthenticationRevalidated: observation.Outcome == scenario.Passed &&
			observation.SessionID != "" && observation.SessionID == e.authenticatedSessionID,
		GameID: proof.GameID, PeerGeneration: proof.PeerGeneration,
		TransportGeneration: proof.TransportGeneration, GameplaySessionID: proof.GameplaySessionID,
		PrepareQueuedAt: proof.PrepareQueuedAt, StatusReceivedAt: proof.StatusReceivedAt,
		AcceptedAt: proof.AcceptedAt, Status: proof.Status, Progress: proof.Progress,
		EffectiveLevelID: proof.EffectiveLevelID, EffectiveMapSeed: proof.EffectiveMapSeed,
		EffectivePrepareMask: proof.EffectivePrepareMask, EffectiveOccurrence: proof.EffectiveOccurrence,
		EffectiveDifficulty: proof.EffectiveDifficulty, PreparePayload: proof.PreparePayload[:],
		StatusPayload: proof.StatusPayload, StatusMessageID: proof.StatusMessageID,
		StatusTraceID: proof.StatusTraceID, StatusSourceTime: proof.StatusSourceTime,
		IsWirePublicationObserved:             proof.IsWirePublicationObserved,
		IsClientSeedEchoObserved:              proof.IsClientSeedEchoObserved,
		IsAuthoritativeZoneGenerationObserved: observation.ZoneGeneration != 0,
		RequestedPopulationProvenance:         e.definition.PopulationProvenance,
		ProfileSetupArtifact:                  e.ProfileArtifact.Path, ProfileSetupSHA256: e.ProfileArtifact.SHA256,
		Effective: scenarioHostEffective(observation.Effective)}
	payload, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("prepareMarshal: %w", err)
	}
	payload = append(payload, '\n')
	e.observationID++
	artifact, err := writeScenarioHostArtifact(e.reportDirectory,
		fmt.Sprintf("prepare-%04d.json", e.observationID), "prepare_acceptance", payload)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("prepareWrite: %w", err)
	}
	return artifact, nil
}

func (e *scenarioHost) persistObservation(kind scenario.StepKind, observation scenario.Observation) (scenario.Observation, error) {
	// This allowlist deliberately excludes account/profile data and credentials.
	response := struct {
		Kind           scenario.StepKind            `json:"kind"`
		ObservedAt     time.Time                    `json:"observed_at"`
		Outcome        scenario.Outcome             `json:"outcome"`
		Detail         string                       `json:"detail"`
		SessionID      string                       `json:"session_id,omitempty"`
		ZoneGeneration uint64                       `json:"zone_generation,omitempty"`
		Effective      *scenarioHostEffectiveInputs `json:"effective,omitempty"`
	}{Kind: kind, ObservedAt: time.Now().UTC(), Outcome: observation.Outcome,
		Detail: observation.Detail, SessionID: observation.SessionID, ZoneGeneration: observation.ZoneGeneration,
		Effective: scenarioHostEffective(observation.Effective)}
	payload, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return scenario.Observation{}, fmt.Errorf("observationMarshal: %w", err)
	}
	payload = append(payload, '\n')
	e.observationID++
	artifact, err := writeScenarioHostArtifact(e.reportDirectory,
		fmt.Sprintf("observation-%04d.json", e.observationID), "server_observation", payload)
	if err != nil {
		return scenario.Observation{}, fmt.Errorf("observationPersist: %w", err)
	}
	observation.Artifacts = append(observation.Artifacts, artifact)
	return observation, nil
}

type scenarioHostEffectiveInputs struct {
	Level                string   `json:"level"`
	Occurrence           uint32   `json:"occurrence"`
	Difficulty           uint32   `json:"difficulty"`
	MapSeed              uint32   `json:"map_seed"`
	PrepareMask          uint32   `json:"prepare_mask"`
	PopulationProvenance string   `json:"population_provenance"`
	UnknownRNGOwners     []string `json:"unknown_rng_owners"`
}

func scenarioHostEffective(effective *scenario.EffectiveInputs) *scenarioHostEffectiveInputs {
	if effective == nil {
		return nil
	}
	return &scenarioHostEffectiveInputs{Level: effective.Level, Occurrence: effective.Occurrence,
		Difficulty: effective.Difficulty, MapSeed: effective.MapSeed, PrepareMask: effective.PrepareMask,
		PopulationProvenance: effective.PopulationProvenance,
		UnknownRNGOwners:     append([]string(nil), effective.UnknownRNGOwners...)}
}

func (e *scenarioHost) persistProfile(profile sporenet.ScenarioProfile) (scenario.Artifact, error) {
	response := struct {
		SchemaVersion              uint32    `json:"schema_version"`
		UserID                     int64     `json:"user_id"`
		ProgressionID              string    `json:"progression_id"`
		SquadID                    string    `json:"squad_id"`
		ActualSquadID              uint32    `json:"actual_squad_id"`
		CreatureIDs                [3]uint32 `json:"creature_ids"`
		HeroNouns                  [3]uint32 `json:"hero_nouns"`
		ChainProgression           uint32    `json:"chain_progression"`
		OnboardingProgress         uint32    `json:"onboarding_progress"`
		AccountLevel               uint32    `json:"account_level"`
		AccountXP                  uint32    `json:"account_xp"`
		IsSuppliedDevelopmentSetup bool      `json:"is_supplied_development_setup"`
		IsTutorialPlayObserved     bool      `json:"is_tutorial_play_observed"`
		IsFirstStagePlayObserved   bool      `json:"is_first_stage_play_observed"`
	}{SchemaVersion: scenario.SchemaVersion, UserID: profile.UserID,
		ProgressionID: profile.ProgressionID, SquadID: profile.SquadID,
		ActualSquadID: profile.ActualSquadID, CreatureIDs: profile.CreatureIDs,
		HeroNouns: profile.HeroNouns, ChainProgression: profile.ChainProgression,
		OnboardingProgress: profile.OnboardingProgress, AccountLevel: profile.AccountLevel,
		AccountXP: profile.AccountXP, IsSuppliedDevelopmentSetup: true}
	payload, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("profileMarshal: %w", err)
	}
	payload = append(payload, '\n')
	artifact, err := writeScenarioHostArtifact(e.reportDirectory, "profile-setup.json", "profile_setup", payload)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("profilePersist: %w", err)
	}
	return artifact, nil
}

func writeScenarioHostArtifact(directory, name, role string, payload []byte) (scenario.Artifact, error) {
	path := filepath.Join(directory, name)
	w, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("artifactCreate: %w", err)
	}
	count, writeErr := w.Write(payload)
	if writeErr == nil && count != len(payload) {
		writeErr = errors.New("scenario artifact returned short write")
	}
	syncErr := w.Sync()
	closeErr := w.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return scenario.Artifact{}, fmt.Errorf("artifactWrite: %w", errors.Join(writeErr, syncErr, closeErr))
	}
	digest := sha256.Sum256(payload)
	return scenario.Artifact{Role: role, Path: path, SHA256: hex.EncodeToString(digest[:])}, nil
}
