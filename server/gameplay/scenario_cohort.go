//go:build scenario

package gameplay

import (
	"errors"
	"fmt"
)

// Called under the gameplay registry lock at queue admission and again at the
// packet owner before mutation. A shared focused spawn needs the complete
// matching live cohort, not just the requester's original cardinality field.
func (e *gameplaySessionRegistry) validateFocusedSpawnCohortLocked(source gameplayPeerSession, runID string) error {
	participantIDs, err := source.binding.FocusedRecipeCohort(runID)
	if err != nil {
		return fmt.Errorf("spawnBoundCohort: %w", err)
	}
	boundRunID, isAmbientSuppressed := source.binding.FocusedRecipeInputs()
	if boundRunID != runID || source.zone == nil {
		return errors.New("focused spawn no longer owns the bound run or zone")
	}
	err = source.zone.ValidateScenarioRecipe(source.binding.RunSeed, isAmbientSuppressed)
	if err != nil {
		return fmt.Errorf("spawnZoneRecipe: %w", err)
	}
	members := source.zone.Snapshot().Members
	if len(members) != len(participantIDs) {
		return errors.New("focused spawn requires every frozen participant in the shared zone")
	}
	expectedUsers := make(map[uint64]bool, len(participantIDs))
	for _, userID := range participantIDs {
		expectedUsers[userID] = true
	}
	for _, member := range members {
		if !member.IsConnected || !expectedUsers[member.UserID] {
			return errors.New("focused spawn zone contains a disconnected or unexpected participant")
		}
		isCurrentFound := false
		for endpoint, candidate := range e.sessions {
			if candidate.zone != source.zone || candidate.binding.UserID != member.UserID || candidate.binding.GameID != source.binding.GameID || candidate.generation != member.PeerGeneration {
				continue
			}
			key := gameplaySessionMemberKey(candidate)
			if candidate.transportGeneration == 0 || e.memberEndpoints[key] != endpoint || e.memberTransports[key] != candidate.transportGeneration {
				continue
			}
			if !candidate.stage.IsDungeon() || !candidate.dungeonSetup.IsCommitted() || candidate.isZoneTerminal() {
				return errors.New("focused spawn participant is not in committed live dungeon gameplay")
			}
			candidateIDs, cohortErr := candidate.binding.FocusedRecipeCohort(runID)
			if cohortErr != nil {
				return fmt.Errorf("spawnPeerCohort: %w", cohortErr)
			}
			candidateRunID, isCandidateSuppressed := candidate.binding.FocusedRecipeInputs()
			if candidateRunID != runID || candidate.binding.RunSeed != source.binding.RunSeed || isCandidateSuppressed != isAmbientSuppressed || len(candidateIDs) != len(participantIDs) {
				return errors.New("focused spawn participant run, seed or population policy changed")
			}
			for index, userID := range participantIDs {
				if candidateIDs[index] != userID {
					return errors.New("focused spawn participant retains a different frozen cohort")
				}
			}
			isCurrentFound = true
			break
		}
		if !isCurrentFound {
			return errors.New("focused spawn participant has no current admitted gameplay transport")
		}
	}
	return nil
}
