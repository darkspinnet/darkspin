//go:build scenario

package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/gameplay"
	"github.com/darkspinnet/darkspin/server/scenario"
)

type scenarioDungeonCommitProvider interface {
	ScenarioDungeonCommitObservation(context.Context, int64) (scenario.Observation, gameplay.ScenarioDungeonCommitEvidence, error)
}

// ScenarioBoundDungeonCommitObservation correlates a retained ordinary server
// commit with a pinned live login. The proof keeps its original gameplay ID;
// client world readiness, control and packet publication remain unobserved.
func (e *Server) ScenarioBoundDungeonCommitObservation(
	ctx context.Context, userID int64, pinnedBlazeID string,
) (scenario.Observation, gameplay.ScenarioDungeonCommitEvidence, error) {
	proof := gameplay.ScenarioDungeonCommitEvidence{}
	if ctx == nil || userID <= 0 || e == nil {
		return scenario.Observation{}, proof, errors.New("bound dungeon observation requires context, isolated identity and server")
	}
	if pinnedBlazeID == "" {
		return scenarioDungeonInconclusive("initial live authentication identity was not pinned"), proof, nil
	}
	authentication, err := e.ScenarioObservation(ctx, userID, scenario.Authenticated)
	if err != nil {
		return scenario.Observation{}, proof, fmt.Errorf("dungeonLogin: %w", err)
	}
	if authentication.Outcome != scenario.Passed || authentication.SessionID != pinnedBlazeID {
		return scenarioDungeonInconclusive("pinned initial Blaze login is no longer the accepted live identity"), proof, nil
	}
	provider, isSupported := e.bugContext.(scenarioDungeonCommitProvider)
	if !isSupported {
		return scenarioDungeonInconclusive("ordinary dungeon commit observation provider is unavailable"), proof, nil
	}
	observation, proof, err := provider.ScenarioDungeonCommitObservation(ctx, userID)
	if err != nil {
		return scenarioDungeonInconclusive("ordinary dungeon commit observation failed"), proof, fmt.Errorf("dungeonGameplay: %w", err)
	}
	if observation.Outcome != scenario.Passed {
		return scenarioDungeonInconclusive(observation.Detail), proof, nil
	}
	if !scenarioDungeonProofMatches(observation, proof, userID) {
		return scenarioDungeonInconclusive("ordinary dungeon commit proof does not identify the isolated live gameplay binding"), proof, nil
	}
	authentication, err = e.ScenarioObservation(ctx, userID, scenario.Authenticated)
	if err != nil {
		return scenarioDungeonInconclusive("live authentication recheck failed during dungeon correlation"), proof, fmt.Errorf("dungeonRecheck: %w", err)
	}
	if authentication.Outcome != scenario.Passed || authentication.SessionID != pinnedBlazeID {
		return scenarioDungeonInconclusive("pinned Blaze authentication changed during dungeon observation"), proof, nil
	}
	currentObservation, currentProof, err := provider.ScenarioDungeonCommitObservation(ctx, userID)
	if err != nil {
		return scenarioDungeonInconclusive("current gameplay commitment recheck failed"), proof, fmt.Errorf("dungeonCurrent: %w", err)
	}
	if currentObservation.Outcome != scenario.Passed ||
		!scenarioDungeonProofMatches(currentObservation, currentProof, userID) ||
		!scenarioDungeonProofSame(proof, currentProof) {
		return scenarioDungeonInconclusive("gameplay commitment or binding changed during authentication correlation; retained proof is historical"), proof, nil
	}
	authentication, err = e.ScenarioObservation(ctx, userID, scenario.Authenticated)
	if err != nil {
		return scenarioDungeonInconclusive("final live authentication recheck failed"), proof, fmt.Errorf("dungeonFinalLogin: %w", err)
	}
	if authentication.Outcome != scenario.Passed || authentication.SessionID != pinnedBlazeID {
		return scenarioDungeonInconclusive("pinned Blaze authentication changed during commitment recheck"), proof, nil
	}
	err = ctx.Err()
	if err != nil {
		return scenarioDungeonInconclusive("dungeon observation deadline reached during correlation"), proof, fmt.Errorf("dungeonFinalContext: %w", err)
	}
	observation.SessionID = pinnedBlazeID
	return observation, proof, nil
}

func scenarioDungeonInconclusive(detail string) scenario.Observation {
	return scenario.Observation{Outcome: scenario.Inconclusive, Detail: detail}
}

func scenarioDungeonProofMatches(observation scenario.Observation, proof gameplay.ScenarioDungeonCommitEvidence, userID int64) bool {
	return proof.UserID == uint64(userID) && proof.GameID != 0 &&
		proof.GameplaySessionID != "" && proof.GameplaySessionID == observation.SessionID &&
		proof.PeerGeneration != 0 && proof.TransportGeneration != 0 &&
		proof.ZoneGeneration != 0 && proof.ZoneGeneration == observation.ZoneGeneration &&
		proof.SetupGeneration != 0 && !proof.CommittedAt.IsZero() && proof.Level != ""
}

func scenarioDungeonProofSame(proof, current gameplay.ScenarioDungeonCommitEvidence) bool {
	return proof.UserID == current.UserID && proof.GameID == current.GameID &&
		proof.GameplaySessionID == current.GameplaySessionID &&
		proof.PeerGeneration == current.PeerGeneration && proof.TransportGeneration == current.TransportGeneration &&
		proof.ZoneGeneration == current.ZoneGeneration && proof.SetupGeneration == current.SetupGeneration &&
		proof.RunSeed == current.RunSeed && proof.CommittedAt.Equal(current.CommittedAt) &&
		proof.Level == current.Level && proof.Occurrence == current.Occurrence &&
		proof.Difficulty == current.Difficulty && proof.MapSeed == current.MapSeed && proof.PrepareMask == current.PrepareMask
}
