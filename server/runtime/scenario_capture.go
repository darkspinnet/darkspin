//go:build scenario

package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/gameplay"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/snapshot"
)

type scenarioFixtureProvider interface {
	ScenarioFixtureCapture(context.Context, int64) (gameplay.ScenarioFixtureEvidence, error)
}

type ScenarioCaptureResult struct {
	Observation                        scenario.Observation
	Fixture                            *gameplay.ScenarioFixtureEvidence
	Dump                               snapshot.ScenarioDump
	FixtureFailure                     string
	DumpFailure                        string
	IsInitialAuthenticationRevalidated bool
}

func (e *Server) ScenarioConfigureCapture(ctx context.Context, req snapshot.ScenarioRecorderRequest) error {
	if e == nil || e.snapshot == nil {
		return errors.New("scenario snapshot service unavailable")
	}
	err := e.snapshot.ConfigureScenario(ctx, req)
	if err != nil {
		return fmt.Errorf("captureConfigure: %w", err)
	}
	return nil
}

// ScenarioCapture produces diagnostics even when gameplay entry is unsupported.
// Retained fixture state only correlates after the pinned login is rechecked;
// a successful diagnostic write never advances an unsupported milestone.
func (e *Server) ScenarioCapture(ctx context.Context, userID int64, authenticatedSessionID string, kind scenario.StepKind) (ScenarioCaptureResult, error) {
	result := ScenarioCaptureResult{Observation: scenario.Observation{Outcome: scenario.Inconclusive,
		Detail: "partial diagnostic capture; native materialization, client readiness and complete fixture parity are unobserved"}}
	if ctx == nil || userID <= 0 || e == nil || e.snapshot == nil {
		return result, errors.New("scenario capture requires context, isolated identity and snapshot service")
	}
	switch kind {
	case scenario.NativeLayout, scenario.TerminalFailure, scenario.FixtureComparison:
	default:
		return result, errors.New("unsupported scenario capture boundary")
	}
	err := ctx.Err()
	if err != nil {
		return result, fmt.Errorf("captureContext: %w", err)
	}
	fixture, fixtureErr := e.scenarioBoundFixture(ctx, userID, authenticatedSessionID)
	if fixtureErr != nil {
		result.FixtureFailure = fixtureErr.Error()
	} else {
		result.Fixture = &fixture
	}
	result.Dump, err = e.snapshot.DumpScenario(ctx, snapshot.ScenarioDumpRequest{UserID: userID, Boundary: string(kind)})
	if err != nil {
		result.DumpFailure = err.Error()
	}
	if result.Fixture == nil {
		return result, nil
	}
	currentFixture, err := e.scenarioBoundFixture(ctx, userID, authenticatedSessionID)
	if err != nil {
		result.FixtureFailure = fmt.Errorf("captureBinding: %w", err).Error()
		return result, nil
	}
	if currentFixture.UserID != fixture.UserID || currentFixture.GameID != fixture.GameID ||
		currentFixture.GameplaySessionID != fixture.GameplaySessionID ||
		currentFixture.PeerGeneration != fixture.PeerGeneration ||
		currentFixture.TransportGeneration != fixture.TransportGeneration ||
		currentFixture.ZoneGeneration != fixture.ZoneGeneration {
		result.FixtureFailure = "gameplay peer, transport or authoritative zone changed during diagnostic collection"
		return result, nil
	}
	authentication, err := e.ScenarioObservation(ctx, userID, scenario.Authenticated)
	if err != nil {
		result.FixtureFailure = fmt.Errorf("captureRecheck: %w", err).Error()
		return result, nil
	}
	if authentication.Outcome != scenario.Passed || authentication.SessionID != authenticatedSessionID {
		result.FixtureFailure = "pinned Blaze login changed during diagnostic collection"
		return result, nil
	}
	result.IsInitialAuthenticationRevalidated = true
	result.Observation.SessionID = authenticatedSessionID
	result.Observation.ZoneGeneration = fixture.ZoneGeneration
	return result, nil
}

func (e *Server) scenarioBoundFixture(ctx context.Context, userID int64, authenticatedSessionID string) (gameplay.ScenarioFixtureEvidence, error) {
	if authenticatedSessionID == "" {
		return gameplay.ScenarioFixtureEvidence{}, errors.New("initial live authentication identity was not pinned")
	}
	authentication, err := e.ScenarioObservation(ctx, userID, scenario.Authenticated)
	if err != nil {
		return gameplay.ScenarioFixtureEvidence{}, fmt.Errorf("fixtureLogin: %w", err)
	}
	if authentication.Outcome != scenario.Passed || authentication.SessionID != authenticatedSessionID {
		return gameplay.ScenarioFixtureEvidence{}, errors.New("pinned initial Blaze login is no longer live")
	}
	provider, isSupported := e.bugContext.(scenarioFixtureProvider)
	if !isSupported {
		return gameplay.ScenarioFixtureEvidence{}, errors.New("authoritative fixture observation provider unavailable")
	}
	fixture, err := provider.ScenarioFixtureCapture(ctx, userID)
	if err != nil {
		return gameplay.ScenarioFixtureEvidence{}, fmt.Errorf("fixtureObserve: %w", err)
	}
	if fixture.UserID != uint64(userID) || fixture.GameID == 0 || fixture.GameplaySessionID == "" ||
		fixture.PeerGeneration == 0 || fixture.TransportGeneration == 0 {
		return gameplay.ScenarioFixtureEvidence{}, errors.New("fixture proof does not identify the isolated live gameplay binding")
	}
	authentication, err = e.ScenarioObservation(ctx, userID, scenario.Authenticated)
	if err != nil {
		return gameplay.ScenarioFixtureEvidence{}, fmt.Errorf("fixtureRecheck: %w", err)
	}
	if authentication.Outcome != scenario.Passed || authentication.SessionID != authenticatedSessionID {
		return gameplay.ScenarioFixtureEvidence{}, errors.New("pinned initial Blaze login changed during fixture observation")
	}
	return fixture, nil
}
