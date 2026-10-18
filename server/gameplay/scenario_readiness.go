//go:build scenario

package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
)

type scenarioPeerReadiness struct {
	scenarioDungeonCommit ScenarioDungeonCommitEvidence
}

// ScenarioDungeonCommitEvidence records only the successful server setup
// transaction. It does not establish packet publication, client world loading,
// controllable heroes, or completeness of the separate fixture capture.
type ScenarioDungeonCommitEvidence struct {
	UserID              uint64
	GameID              uint32
	GameplaySessionID   string
	PeerGeneration      uint64
	TransportGeneration uint64
	ZoneGeneration      uint64
	SetupGeneration     uint64
	RunSeed             uint64
	CommittedAt         time.Time
	Level               string
	Occurrence          uint32
	Difficulty          uint32
	MapSeed             uint32
	PrepareMask         uint32
}

type scenarioDungeonCommitProvider interface {
	ScenarioDungeonCommitObservation(context.Context, int64) (scenario.Observation, ScenarioDungeonCommitEvidence, error)
}

func (e Lifecycle) ScenarioDungeonCommitObservation(
	ctx context.Context, userID int64,
) (scenario.Observation, ScenarioDungeonCommitEvidence, error) {
	if ctx == nil || userID <= 0 {
		return scenario.Observation{}, ScenarioDungeonCommitEvidence{}, errors.New("dungeon observation requires context and isolated user")
	}
	err := ctx.Err()
	if err != nil {
		return scenario.Observation{}, ScenarioDungeonCommitEvidence{}, fmt.Errorf("dungeonContext: %w", err)
	}
	provider, isSupported := e.syncSnapshot.(scenarioDungeonCommitProvider)
	if !isSupported {
		return scenario.Observation{Outcome: scenario.Inconclusive,
			Detail: "ordinary dungeon commit observation provider is unavailable"}, ScenarioDungeonCommitEvidence{}, nil
	}
	observation, evidence, err := provider.ScenarioDungeonCommitObservation(ctx, userID)
	if err != nil {
		return scenario.Observation{}, ScenarioDungeonCommitEvidence{}, fmt.Errorf("dungeonProvider: %w", err)
	}
	return observation, evidence, nil
}

// The caller holds the existing registry lock and has just successfully
// committed this exact ordinary setup reservation. No additional gameplay
// operation, network write, or client readiness transition occurs here.
func (e *gameplayPeerSession) recordScenarioDungeonCommit(setupEpoch uint64) {
	if e == nil || setupEpoch == 0 || !e.dungeonSetup.IsCommittedGeneration(setupEpoch) ||
		!e.stage.IsDungeon() || e.isRejoinPending || e.isZoneTerminal() || !e.scenarioFixtures.isCurrent(*e) ||
		!e.isScenarioFixtureZoneCurrent() {
		return
	}
	mapSeed, prepareMask, isConfigured, err := e.binding.ScenarioMapInputs()
	if err != nil {
		// This observational producer must not alter the ordinary commit. An
		// invalid binding withholds its proof; the reader reports inconclusive.
		return
	}
	if !isConfigured || mapSeed != e.scenarioFixtures.mapSeed ||
		prepareMask != e.scenarioFixtures.prepareMask {
		return
	}
	if e.scenarioDungeonCommit.SetupGeneration == setupEpoch &&
		e.scenarioDungeonCommit.isCurrent(*e) {
		return
	}
	e.scenarioDungeonCommit = ScenarioDungeonCommitEvidence{
		UserID: e.binding.UserID, GameID: e.binding.GameID,
		GameplaySessionID: fmt.Sprintf("game:%d/peer:%d/transport:%d",
			e.binding.GameID, e.generation, e.transportGeneration),
		PeerGeneration: e.generation, TransportGeneration: e.transportGeneration,
		ZoneGeneration: e.scenarioFixtures.zoneGeneration, SetupGeneration: setupEpoch,
		RunSeed: e.binding.RunSeed, CommittedAt: time.Now().UTC(),
		Level: e.binding.Level, Occurrence: e.binding.ChainLevelIndex,
		Difficulty: e.binding.Difficulty, MapSeed: mapSeed, PrepareMask: prepareMask,
	}
}

func (e ScenarioDungeonCommitEvidence) isCurrent(peerSession gameplayPeerSession) bool {
	return e.UserID != 0 && e.UserID == peerSession.binding.UserID &&
		e.GameID != 0 && e.GameID == peerSession.binding.GameID &&
		e.PeerGeneration != 0 && e.PeerGeneration == peerSession.generation &&
		e.TransportGeneration != 0 && e.TransportGeneration == peerSession.transportGeneration &&
		e.ZoneGeneration != 0 && e.ZoneGeneration == peerSession.scenarioFixtures.zoneGeneration &&
		e.SetupGeneration != 0 && peerSession.dungeonSetup.IsCommittedGeneration(e.SetupGeneration) &&
		e.RunSeed == peerSession.binding.RunSeed && e.Level == peerSession.binding.Level &&
		e.Occurrence == peerSession.binding.ChainLevelIndex && e.Difficulty == peerSession.binding.Difficulty
}

func (e *gameplaySessionRegistry) ScenarioDungeonCommitObservation(
	ctx context.Context, userID int64,
) (scenario.Observation, ScenarioDungeonCommitEvidence, error) {
	if ctx == nil || userID <= 0 {
		return scenario.Observation{}, ScenarioDungeonCommitEvidence{}, errors.New("dungeon observation requires context and isolated user")
	}
	if e == nil {
		return scenario.Observation{Outcome: scenario.Inconclusive,
			Detail: "live gameplay registry is unavailable"}, ScenarioDungeonCommitEvidence{}, nil
	}
	err := ctx.Err()
	if err != nil {
		return scenario.Observation{}, ScenarioDungeonCommitEvidence{}, fmt.Errorf("dungeonReadContext: %w", err)
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !e.mutex.TryRLock() {
		select {
		case <-ctx.Done():
			return scenario.Observation{}, ScenarioDungeonCommitEvidence{}, fmt.Errorf("dungeonLock: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	defer e.mutex.RUnlock()
	peerSession, observation, err := e.scenarioSessionLocked(ctx, userID)
	if err != nil {
		return scenario.Observation{}, ScenarioDungeonCommitEvidence{}, fmt.Errorf("dungeonSession: %w", err)
	}
	if observation.Outcome != scenario.Passed {
		return observation, ScenarioDungeonCommitEvidence{}, nil
	}
	observation.Outcome = scenario.Inconclusive
	observation.Detail = "ordinary dungeon setup commit has not been retained for the current live scenario binding"
	evidence := peerSession.scenarioDungeonCommit
	if !evidence.isCurrent(peerSession) || !peerSession.stage.IsDungeon() ||
		peerSession.isRejoinPending || peerSession.isZoneTerminal() || evidence.CommittedAt.IsZero() ||
		!peerSession.scenarioFixtures.isCurrent(peerSession) || !peerSession.isScenarioFixtureZoneCurrent() {
		return observation, ScenarioDungeonCommitEvidence{}, nil
	}
	mapSeed, prepareMask, isConfigured, err := peerSession.binding.ScenarioMapInputs()
	if err != nil {
		return scenario.Observation{}, ScenarioDungeonCommitEvidence{}, fmt.Errorf("dungeonBinding: %w", err)
	}
	if !isConfigured || mapSeed != evidence.MapSeed || prepareMask != evidence.PrepareMask ||
		mapSeed != peerSession.scenarioFixtures.mapSeed || prepareMask != peerSession.scenarioFixtures.prepareMask ||
		evidence.GameplaySessionID != observation.SessionID {
		return observation, ScenarioDungeonCommitEvidence{}, nil
	}
	err = ctx.Err()
	if err != nil {
		return scenario.Observation{}, ScenarioDungeonCommitEvidence{}, fmt.Errorf("dungeonRecheck: %w", err)
	}
	observation.Outcome = scenario.Passed
	observation.Detail = "retained successful ordinary server dungeon setup commit; packet publication, client world readiness and hero control are unobserved"
	observation.ZoneGeneration = evidence.ZoneGeneration
	return observation, evidence, nil
}
