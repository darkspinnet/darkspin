//go:build scenario

package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/buildinfo"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/gameplay"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

type scenarioObservationProvider interface {
	ScenarioObservation(context.Context, int64, scenario.StepKind) (scenario.Observation, error)
}

type scenarioPrepareProvider interface {
	ScenarioPrepareObservation(context.Context, int64) (scenario.Observation, gameplay.ScenarioPrepareEvidence, error)
}

func (e *Server) ScenarioCapability() scenario.Capability {
	return scenario.NewCapability("server", buildinfo.ID)
}

// ScenarioRegister creates the feature-owned fresh development profile and
// installs requested inputs before any client launch or gameplay admission.
func (e *Server) ScenarioRegister(
	ctx context.Context, loginName, password, runID string, definition scenario.Definition,
) (sporenet.ScenarioProfile, error) {
	if ctx == nil {
		return sporenet.ScenarioProfile{}, errors.New("scenario registration requires context")
	}
	err := ctx.Err()
	if err != nil {
		return sporenet.ScenarioProfile{}, fmt.Errorf("registrationContext: %w", err)
	}
	if e == nil || e.userManager == nil || e.gameManager == nil {
		return sporenet.ScenarioProfile{}, errors.New("scenario account or game manager unavailable")
	}
	err = definition.Validate()
	if err != nil {
		return sporenet.ScenarioProfile{}, fmt.Errorf("registrationDefinition: %w", err)
	}
	if runID == "" || len(runID) > 80 {
		return sporenet.ScenarioProfile{}, errors.New("scenario run identity is invalid")
	}
	for _, character := range runID {
		isLetter := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		isDigit := character >= '0' && character <= '9'
		if !isLetter && !isDigit && character != '-' && character != '_' {
			return sporenet.ScenarioProfile{}, errors.New("scenario run identity contains unsupported characters")
		}
	}
	level, isSelected := e.gameManager.ChainLevelForSelection(definition.Occurrence)
	if !isSelected || level != definition.Level {
		return sporenet.ScenarioProfile{}, errors.New("scenario authored campaign occurrence mismatch")
	}
	heroNouns := [3]uint32{}
	for index, heroID := range definition.HeroIDs {
		if heroID > uint64(^uint32(0)) {
			return sporenet.ScenarioProfile{}, errors.New("scenario hero noun exceeds uint32")
		}
		heroNouns[index] = uint32(heroID)
	}
	profile, err := e.userManager.CreateScenarioProfile(ctx, sporenet.ScenarioProfileRequest{
		LoginName: loginName, Password: password, ProgressionID: definition.ProgressionID,
		SquadID: definition.SquadID, HeroNouns: heroNouns,
	})
	if err != nil {
		return sporenet.ScenarioProfile{}, fmt.Errorf("profileCreate: %w", err)
	}
	if profile.UserID <= 0 {
		return sporenet.ScenarioProfile{}, errors.New("scenario registration produced no account")
	}
	err = e.gameManager.ConfigureScenario(ctx, game.ScenarioMapRequest{
		UserID: profile.UserID, RunID: runID, Level: definition.Level,
		Occurrence: definition.Occurrence, Difficulty: definition.Difficulty,
		MapSeed: definition.MapSeed, PrepareMask: definition.PrepareMask,
		SquadID: profile.ActualSquadID, HeroNouns: profile.HeroNouns,
	})
	if err != nil {
		return sporenet.ScenarioProfile{}, fmt.Errorf("profileMap: %w", err)
	}
	err = e.scenarioContinue(ctx, loginName, runID, profile)
	if err != nil {
		return sporenet.ScenarioProfile{}, fmt.Errorf("profileContinue: %w", err)
	}
	return profile, nil
}

func (e *Server) ScenarioObservation(ctx context.Context, userID int64, kind scenario.StepKind) (scenario.Observation, error) {
	if ctx == nil {
		return scenario.Observation{}, errors.New("scenario observation requires context")
	}
	err := ctx.Err()
	if err != nil {
		return scenario.Observation{}, fmt.Errorf("observationContext: %w", err)
	}
	if userID <= 0 {
		return scenario.Observation{}, errors.New("scenario observation requires an isolated user identity")
	}
	if e == nil {
		return scenario.Observation{}, errors.New("scenario server unavailable")
	}
	if kind == scenario.Authenticated {
		providers := make([]scenarioObservationProvider, 0, len(e.blazeServers)+len(e.sharedTCPServers))
		for _, service := range e.blazeServers {
			providers = append(providers, service)
		}
		for _, service := range e.sharedTCPServers {
			providers = append(providers, service)
		}
		observation := scenario.Observation{Outcome: scenario.Inconclusive,
			Detail: "no accepted live Blaze login for the isolated account"}
		for _, provider := range providers {
			candidate, err := provider.ScenarioObservation(ctx, userID, kind)
			if err != nil {
				return scenario.Observation{}, fmt.Errorf("loginObserve: %w", err)
			}
			if candidate.Outcome != scenario.Passed {
				continue
			}
			if observation.Outcome == scenario.Passed {
				return scenario.Observation{Outcome: scenario.Inconclusive,
					Detail: "multiple accepted live Blaze logins for the isolated account"}, nil
			}
			observation = candidate
		}
		return observation, nil
	}
	provider, isAvailable := e.bugContext.(scenarioObservationProvider)
	if !isAvailable {
		return scenario.Observation{Outcome: scenario.Inconclusive, Detail: "gameplay scenario observation provider unavailable"}, nil
	}
	observation, err := provider.ScenarioObservation(ctx, userID, kind)
	if err != nil {
		return scenario.Observation{}, fmt.Errorf("gameplayObserve: %w", err)
	}
	return observation, nil
}

// ScenarioBoundObservation preserves the pinned initial login identity only
// after verifying that it is still live for this isolated user. Prepare callers
// that persist raw gameplay proof use ScenarioBoundPrepareObservation directly.
func (e *Server) ScenarioBoundObservation(
	ctx context.Context, userID int64, authenticatedSessionID string, kind scenario.StepKind,
) (scenario.Observation, error) {
	if ctx == nil || userID <= 0 {
		return scenario.Observation{}, errors.New("bound observation requires context and isolated identity")
	}
	if kind == scenario.Authenticated {
		observation, err := e.ScenarioObservation(ctx, userID, kind)
		if err != nil {
			return scenario.Observation{}, fmt.Errorf("boundLogin: %w", err)
		}
		return observation, nil
	}
	if kind == scenario.PrepareAccepted {
		observation, evidence, err := e.ScenarioBoundPrepareObservation(ctx, userID, authenticatedSessionID)
		if err != nil {
			return scenario.Observation{}, fmt.Errorf("boundPrepare: %w", err)
		}
		if observation.Outcome == scenario.Passed && evidence.GameplaySessionID == "" {
			return scenario.Observation{}, errors.New("scenario prepare proof lacks gameplay identity")
		}
		return observation, nil
	}
	if kind == scenario.DungeonCommitted {
		observation, evidence, err := e.ScenarioBoundDungeonCommitObservation(ctx, userID, authenticatedSessionID)
		if err != nil {
			return observation, fmt.Errorf("boundDungeon: %w", err)
		}
		if observation.Outcome == scenario.Passed && evidence.GameplaySessionID == "" {
			return scenario.Observation{}, errors.New("scenario dungeon commit proof lacks gameplay identity")
		}
		return observation, nil
	}
	return scenario.Observation{Outcome: scenario.Inconclusive,
		Detail: "correlated gameplay observation is not implemented for this boundary"}, nil
}

// ScenarioBoundPrepareObservation retrieves the current authorized gameplay
// journal, retaining its independent peer identity in typed evidence. Account
// IDs correlate feature authorization; they never stand in for a live login.
func (e *Server) ScenarioBoundPrepareObservation(
	ctx context.Context, userID int64, authenticatedSessionID string,
) (scenario.Observation, gameplay.ScenarioPrepareEvidence, error) {
	proof := gameplay.ScenarioPrepareEvidence{}
	if ctx == nil || userID <= 0 {
		return scenario.Observation{}, proof, errors.New("bound prepare requires context and isolated identity")
	}
	if authenticatedSessionID == "" {
		return scenario.Observation{Outcome: scenario.Inconclusive,
			Detail: "initial live authentication identity was not pinned"}, proof, nil
	}
	authentication, err := e.ScenarioObservation(ctx, userID, scenario.Authenticated)
	if err != nil {
		return scenario.Observation{}, proof, fmt.Errorf("prepareLogin: %w", err)
	}
	if authentication.Outcome != scenario.Passed || authentication.SessionID != authenticatedSessionID {
		return scenario.Observation{Outcome: scenario.Inconclusive,
			Detail: "pinned initial Blaze login is no longer the accepted live identity"}, proof, nil
	}
	provider, isSupported := e.bugContext.(scenarioPrepareProvider)
	if !isSupported {
		return scenario.Observation{Outcome: scenario.Inconclusive,
			Detail: "ordinary prepare observation provider is unavailable"}, proof, nil
	}
	observation, proof, err := provider.ScenarioPrepareObservation(ctx, userID)
	if err != nil {
		return scenario.Observation{}, proof, fmt.Errorf("prepareGameplay: %w", err)
	}
	if observation.Outcome != scenario.Passed {
		return observation, proof, nil
	}
	if observation.SessionID == "" || proof.GameplaySessionID != observation.SessionID || proof.UserID != uint64(userID) {
		return scenario.Observation{Outcome: scenario.Inconclusive,
			Detail: "ordinary prepare proof does not identify the isolated gameplay binding"}, proof, nil
	}
	authentication, err = e.ScenarioObservation(ctx, userID, scenario.Authenticated)
	if err != nil {
		return scenario.Observation{}, proof, fmt.Errorf("prepareRecheck: %w", err)
	}
	if authentication.Outcome != scenario.Passed || authentication.SessionID != authenticatedSessionID {
		return scenario.Observation{Outcome: scenario.Inconclusive,
			Detail: "pinned Blaze authentication changed during gameplay observation"}, proof, nil
	}
	observation.SessionID = authenticatedSessionID
	return observation, proof, nil
}
