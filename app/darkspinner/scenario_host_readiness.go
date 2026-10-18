//go:build scenario

package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/gameplay"
	"github.com/darkspinnet/darkspin/server/scenario"
)

func (e *scenarioHost) persistDungeonCommit(
	proof gameplay.ScenarioDungeonCommitEvidence, observation scenario.Observation,
) (scenario.Artifact, error) {
	// Explicitly allowlist the retained server transaction, preserving its
	// independent gameplay ID even when live correlation became inconclusive.
	response := struct {
		SchemaVersion                      uint32                       `json:"schema_version"`
		RunID                              string                       `json:"run_id"`
		Outcome                            scenario.Outcome             `json:"outcome"`
		Detail                             string                       `json:"detail"`
		AuthenticatedSessionID             string                       `json:"authenticated_session_id"`
		IsInitialAuthenticationRevalidated bool                         `json:"is_initial_authentication_revalidated"`
		UserID                             uint64                       `json:"user_id"`
		GameID                             uint32                       `json:"game_id"`
		GameplaySessionID                  string                       `json:"gameplay_session_id"`
		PeerGeneration                     uint64                       `json:"peer_generation"`
		TransportGeneration                uint64                       `json:"transport_generation"`
		ZoneGeneration                     uint64                       `json:"zone_generation"`
		SetupGeneration                    uint64                       `json:"setup_generation"`
		RunSeed                            uint64                       `json:"run_seed"`
		CommittedAt                        time.Time                    `json:"committed_at"`
		Level                              string                       `json:"level"`
		Occurrence                         uint32                       `json:"occurrence"`
		Difficulty                         uint32                       `json:"difficulty"`
		MapSeed                            uint32                       `json:"map_seed"`
		PrepareMask                        uint32                       `json:"prepare_mask"`
		IsWirePublicationObserved          bool                         `json:"is_wire_publication_observed"`
		IsClientWorldReadinessObserved     bool                         `json:"is_client_world_readiness_observed"`
		IsControllableHeroObserved         bool                         `json:"is_controllable_hero_observed"`
		RequestedPopulationProvenance      string                       `json:"requested_population_provenance"`
		Effective                          *scenarioHostEffectiveInputs `json:"effective,omitempty"`
	}{SchemaVersion: scenario.SchemaVersion, RunID: e.runID, Outcome: observation.Outcome, Detail: observation.Detail,
		AuthenticatedSessionID: e.authenticatedSessionID,
		IsInitialAuthenticationRevalidated: observation.Outcome == scenario.Passed &&
			observation.SessionID != "" && observation.SessionID == e.authenticatedSessionID,
		UserID: proof.UserID, GameID: proof.GameID, GameplaySessionID: proof.GameplaySessionID,
		PeerGeneration: proof.PeerGeneration, TransportGeneration: proof.TransportGeneration,
		ZoneGeneration: proof.ZoneGeneration, SetupGeneration: proof.SetupGeneration, RunSeed: proof.RunSeed,
		CommittedAt: proof.CommittedAt, Level: proof.Level, Occurrence: proof.Occurrence,
		Difficulty: proof.Difficulty, MapSeed: proof.MapSeed, PrepareMask: proof.PrepareMask,
		RequestedPopulationProvenance: e.definition.PopulationProvenance,
		Effective:                     scenarioHostEffective(observation.Effective)}
	payload, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("commitMarshal: %w", err)
	}
	payload = append(payload, '\n')
	e.observationID++
	artifact, err := writeScenarioHostArtifact(e.reportDirectory,
		fmt.Sprintf("dungeon-commit-%04d.json", e.observationID), "dungeon_commit", payload)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("commitWrite: %w", err)
	}
	return artifact, nil
}
