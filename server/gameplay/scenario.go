//go:build scenario

package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
)

type scenarioObservationProvider interface {
	ScenarioObservation(context.Context, int64, scenario.StepKind) (scenario.Observation, error)
}

// ScenarioObservation reads the provider already registered by NewHandler.
// It never admits a player, changes readiness, or interprets account creation
// or JWT issuance as a client authentication observation.
func (e Lifecycle) ScenarioObservation(
	ctx context.Context, userID int64, kind scenario.StepKind,
) (scenario.Observation, error) {
	if ctx == nil {
		return scenario.Observation{}, errors.New("scenario observation requires context")
	}
	err := ctx.Err()
	if err != nil {
		return scenario.Observation{}, fmt.Errorf("observationContext: %w", err)
	}
	if userID <= 0 {
		return scenario.Observation{}, errors.New("scenario observation requires isolated user identity")
	}
	provider, isSupported := e.syncSnapshot.(scenarioObservationProvider)
	if !isSupported {
		return scenario.Observation{
			Outcome: scenario.Inconclusive,
			Detail:  "live gameplay observation provider is unavailable",
		}, nil
	}
	observation, err := provider.ScenarioObservation(ctx, userID, kind)
	if err != nil {
		return scenario.Observation{}, fmt.Errorf("observationProvider: %w", err)
	}
	return observation, nil
}

// ScenarioObservation reads accepted observations from the current authorized
// gameplay binding. The host owns polling and immutable evidence persistence.
func (e *gameplaySessionRegistry) ScenarioObservation(
	ctx context.Context, userID int64, kind scenario.StepKind,
) (scenario.Observation, error) {
	if kind == scenario.DungeonCommitted {
		observation, evidence, err := e.ScenarioDungeonCommitObservation(ctx, userID)
		if err != nil {
			return scenario.Observation{}, fmt.Errorf("dungeonObserve: %w", err)
		}
		if evidence.GameplaySessionID != "" && evidence.GameplaySessionID != observation.SessionID {
			return scenario.Observation{}, errors.New("dungeon observation identity mismatch")
		}
		return observation, nil
	}
	if kind == scenario.PrepareAccepted {
		observation, evidence, err := e.ScenarioPrepareObservation(ctx, userID)
		if err != nil {
			return scenario.Observation{}, fmt.Errorf("prepareObserve: %w", err)
		}
		// The typed companion is consumed by the isolated host evidence adapter.
		// The feature observation remains transport-neutral.
		if evidence.GameplaySessionID != "" && evidence.GameplaySessionID != observation.SessionID {
			return scenario.Observation{}, errors.New("prepare observation identity mismatch")
		}
		return observation, nil
	}
	if kind != scenario.Authenticated {
		return scenario.Observation{
			Outcome: scenario.Inconclusive,
			Detail:  fmt.Sprintf("gameplay observation for %s is not implemented", kind),
		}, nil
	}
	peerSession, observation, err := e.scenarioSession(ctx, userID)
	if err != nil {
		return scenario.Observation{}, fmt.Errorf("helloObserve: %w", err)
	}
	if observation.Outcome == scenario.Passed && peerSession.binding.UserID != uint64(userID) {
		return scenario.Observation{}, errors.New("gameplay observation identity mismatch")
	}
	return observation, nil
}

func (e *gameplaySessionRegistry) scenarioSession(
	ctx context.Context, userID int64,
) (gameplayPeerSession, scenario.Observation, error) {
	observation := scenario.Observation{
		Outcome: scenario.Inconclusive,
		Detail:  "no accepted live gameplay hello for the isolated user",
	}
	if ctx == nil || userID <= 0 {
		return gameplayPeerSession{}, scenario.Observation{}, errors.New("scenario observation requires context and isolated user")
	}
	if e == nil {
		return gameplayPeerSession{}, observation, nil
	}
	err := ctx.Err()
	if err != nil {
		return gameplayPeerSession{}, scenario.Observation{}, fmt.Errorf("registryContext: %w", err)
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !e.mutex.TryRLock() {
		select {
		case <-ctx.Done():
			return gameplayPeerSession{}, scenario.Observation{}, fmt.Errorf("registryLock: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	defer e.mutex.RUnlock()
	peerSession, observation, err := e.scenarioSessionLocked(ctx, userID)
	if err != nil {
		return gameplayPeerSession{}, scenario.Observation{}, fmt.Errorf("registryRead: %w", err)
	}
	return peerSession, observation, nil
}

// The caller holds the registry read or write lock throughout its use of the
// returned peer. Readiness observations use this to validate live membership
// and the retained setup generation in one registry critical section.
func (e *gameplaySessionRegistry) scenarioSessionLocked(
	ctx context.Context, userID int64,
) (gameplayPeerSession, scenario.Observation, error) {
	observation := scenario.Observation{Outcome: scenario.Inconclusive,
		Detail: "no accepted live gameplay hello for the isolated user"}
	if e.isClosed {
		observation.Detail = "gameplay registry is closed"
		return gameplayPeerSession{}, observation, nil
	}
	var selectedSession gameplayPeerSession
	var selectedEndpoint string
	matchedSessionCount := 0
	for endpoint, peerSession := range e.sessions {
		if peerSession.binding.UserID != uint64(userID) {
			continue
		}
		matchedSessionCount++
		selectedSession = peerSession
		selectedEndpoint = endpoint
	}
	err := ctx.Err()
	if err != nil {
		return gameplayPeerSession{}, scenario.Observation{}, fmt.Errorf("registryScan: %w", err)
	}
	if matchedSessionCount == 0 {
		return gameplayPeerSession{}, observation, nil
	}
	if matchedSessionCount != 1 {
		observation.Detail = "multiple live gameplay bindings for the isolated user"
		return gameplayPeerSession{}, observation, nil
	}
	if selectedSession.binding.GameID == 0 || selectedSession.generation == 0 ||
		selectedSession.transportGeneration == 0 {
		observation.Detail = "live gameplay binding has incomplete identity generations"
		return gameplayPeerSession{}, observation, nil
	}
	memberKey := gameplaySessionMemberKey(selectedSession)
	if e.memberEndpoints[memberKey] != selectedEndpoint ||
		e.memberTransports[memberKey] != selectedSession.transportGeneration {
		observation.Detail = "live gameplay binding is not the current admitted transport"
		return gameplayPeerSession{}, observation, nil
	}
	observation.Outcome = scenario.Passed
	observation.Detail = "accepted live gameplay hello observed for the isolated user"
	observation.SessionID = fmt.Sprintf(
		"game:%d/peer:%d/transport:%d", selectedSession.binding.GameID,
		selectedSession.generation, selectedSession.transportGeneration,
	)
	// Authentication does not prove a zone was created. Peer and transport
	// generations identify this session; neither is an authoritative zone epoch.
	return selectedSession, observation, nil
}
