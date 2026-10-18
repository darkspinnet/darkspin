//go:build scenario

package scenario

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Runner struct {
	store          Store
	capabilityPort CapabilityPort
	launcher       Launcher
	observer       Observer
}

func NewRunner(store Store, capabilityPort CapabilityPort, launcher Launcher, observer Observer) *Runner {
	return &Runner{store: store, capabilityPort: capabilityPort, launcher: launcher, observer: observer}
}

func (e *Runner) Run(ctx context.Context, req RunRequest) (Manifest, error) {
	err := req.Definition.Validate()
	if err != nil {
		return Manifest{}, fmt.Errorf("runDefinition: %w", err)
	}
	if e.store == nil {
		return Manifest{}, errors.New("scenario manifest store unavailable")
	}
	paths, err := e.store.Allocate(ctx, req.Definition)
	if err != nil {
		return Manifest{}, fmt.Errorf("runAllocate: %w", err)
	}
	manifest := Manifest{
		SchemaVersion: SchemaVersion, RunID: paths.RunID, Definition: req.Definition,
		Input: req.Input, Paths: paths, StartedAt: time.Now().UTC(), Outcome: NotReached,
		Detail: "allocated; live capability verification not reached",
	}
	for _, milestone := range req.Definition.Milestones {
		manifest.Steps = append(manifest.Steps, StepStatus{Kind: milestone.Kind, Outcome: NotReached})
	}
	err = e.checkpoint(ctx, manifest)
	if err != nil {
		return manifest, fmt.Errorf("runInitialSave: %w", err)
	}
	input, err := e.store.PreserveInput(ctx, paths, req.Input)
	if err != nil {
		return e.finish(ctx, manifest, Failed, fmt.Errorf("runInput: %w", err))
	}
	manifest.Input = input
	err = e.checkpoint(ctx, manifest)
	if err != nil {
		return e.finish(ctx, manifest, Failed, fmt.Errorf("runInputSave: %w", err))
	}
	err = e.verify(ctx, &manifest)
	if err != nil {
		return e.finish(ctx, manifest, Inconclusive, err)
	}
	if e.launcher == nil {
		return e.finish(ctx, manifest, Inconclusive, errors.New("normal-flow isolated launcher adapter unavailable"))
	}
	if e.observer == nil {
		return e.finish(ctx, manifest, Inconclusive, errors.New("live readiness and lifecycle capture adapter unavailable"))
	}
	for index, milestone := range req.Definition.Milestones {
		manifest.Steps[index].StartedAt = time.Now().UTC()
		manifest.Steps[index].Detail = "awaiting observed boundary"
		manifest.Detail = "awaiting " + string(milestone.Kind)
		err = e.store.Save(ctx, manifest)
		if err != nil {
			return e.finish(ctx, manifest, Failed, fmt.Errorf("stepStartSave: %w", err))
		}
		stepContext, cancel := context.WithTimeout(ctx, milestone.Timeout)
		observation, versions, stepErr := e.execute(ctx, stepContext, manifest, milestone)
		contextErr := stepContext.Err()
		cancel()
		if stepErr == nil && contextErr != nil {
			stepErr = fmt.Errorf("stepDeadline: %w", contextErr)
		}
		if stepErr == nil && milestone.Kind == Launch && observation.Outcome == Passed {
			stepErr = validateVersions(versions)
		}
		if stepErr == nil {
			stepErr = validateObservation(req.Definition, milestone.Kind, observation)
		}
		if stepErr == nil && observation.Outcome == Passed {
			stepErr = e.store.VerifyEvidence(ctx, paths, observation.Artifacts)
			if stepErr != nil {
				stepErr = fmt.Errorf("stepEvidence: %w", stepErr)
			}
		}
		if stepErr == nil && observation.Outcome == Passed {
			stepErr = validateCorrelation(manifest, milestone.Kind, observation)
		}
		if stepErr != nil {
			observation.Outcome = Failed
			observation.Detail = stepErr.Error()
		}
		manifest.Steps[index].FinishedAt = time.Now().UTC()
		manifest.Steps[index].Outcome = observation.Outcome
		manifest.Steps[index].Detail = observation.Detail
		manifest.Steps[index].Observation = observation
		manifest.Versions = append(manifest.Versions, versions...)
		manifest.Artifacts = append(manifest.Artifacts, observation.Artifacts...)
		if observation.Effective != nil {
			manifest.Effective = observation.Effective
		}
		if observation.Outcome == Passed {
			manifest.LastReached = milestone.Kind
		}
		if stepErr != nil || observation.Outcome != Passed {
			if stepErr == nil {
				stepErr = fmt.Errorf("milestone %s %s: %s", milestone.Kind, observation.Outcome, observation.Detail)
			}
			// Persist failure before terminal capture or cleanup can block.
			manifest.Outcome = observation.Outcome
			manifest.Detail = stepErr.Error()
			checkpointErr := e.checkpoint(ctx, manifest)
			if checkpointErr != nil {
				stepErr = errors.Join(stepErr, fmt.Errorf("failedCheckpoint: %w", checkpointErr))
			}
			e.captureFailure(ctx, &manifest)
			return e.finish(ctx, manifest, observation.Outcome, fmt.Errorf("runStep: %w", stepErr))
		}
		err = e.store.Save(ctx, manifest)
		if err != nil {
			e.captureFailure(ctx, &manifest)
			return e.finish(ctx, manifest, Failed, fmt.Errorf("stepEndSave: %w", err))
		}
	}
	return e.finish(ctx, manifest, Passed, nil)
}

func (e *Runner) verify(ctx context.Context, manifest *Manifest) error {
	if e.capabilityPort == nil {
		return errors.New("live runner/server/launcher/Fang capability adapter unavailable; automation was not started")
	}
	verifyContext, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	capabilities, err := e.capabilityPort.Verify(verifyContext, CapabilityRequest{
		RunID: manifest.RunID, Paths: manifest.Paths, Definition: manifest.Definition,
	})
	manifest.Capabilities = capabilities
	if err != nil {
		return fmt.Errorf("peerVerify: %w", err)
	}
	err = verifyContext.Err()
	if err != nil {
		return fmt.Errorf("peerDeadline: %w", err)
	}
	err = ValidateCapabilities(capabilities)
	if err != nil {
		return fmt.Errorf("peerCapability: %w", err)
	}
	return nil
}

func ValidateCapabilities(capabilities []Capability) error {
	components := map[string]bool{"runner": false, "server": false, "launcher": false, "fang": false}
	buildID := ""
	for _, capability := range capabilities {
		isSeen, isRequired := components[capability.Component]
		if !isRequired || isSeen {
			return fmt.Errorf("unexpected or duplicate capability component %q", capability.Component)
		}
		if capability.ProtocolVersion != 1 || capability.BuildID == "" {
			return fmt.Errorf("incompatible capability for %s", capability.Component)
		}
		isScenario := false
		for _, feature := range capability.Features {
			if feature == "scenario-v1" {
				isScenario = true
			}
		}
		if !isScenario {
			return fmt.Errorf("compiled scenario support missing for %s", capability.Component)
		}
		if buildID != "" && buildID != capability.BuildID {
			return errors.New("scenario peer build IDs differ")
		}
		buildID = capability.BuildID
		components[capability.Component] = true
	}
	for _, component := range []string{"runner", "server", "launcher", "fang"} {
		if !components[component] {
			return fmt.Errorf("live capability missing for %s", component)
		}
	}
	return nil
}

func (e *Runner) execute(runContext context.Context, stepContext context.Context, manifest Manifest, milestone Milestone) (Observation, []Version, error) {
	if milestone.Kind == Launch {
		deadline, isDeadline := stepContext.Deadline()
		if !isDeadline {
			return Observation{}, nil, errors.New("launch requires a wall deadline")
		}
		// Process lifetime follows runContext; the adapter must independently
		// bound initialization by Deadline and keep ownership until Close.
		result, err := e.launcher.Launch(runContext, LaunchRequest{
			RunID: manifest.RunID, Paths: manifest.Paths, Definition: manifest.Definition,
			Deadline: deadline,
		})
		if err != nil {
			return result.Observation, result.Versions, fmt.Errorf("stepLaunch: %w", err)
		}
		return result.Observation, result.Versions, nil
	}
	observation, err := e.observer.Observe(stepContext, ObservationRequest{
		RunID: manifest.RunID, Paths: manifest.Paths, Definition: manifest.Definition, Milestone: milestone,
	})
	if err != nil {
		return observation, nil, fmt.Errorf("stepObserve: %w", err)
	}
	return observation, nil, nil
}

func validateObservation(definition Definition, kind StepKind, observation Observation) error {
	if observation.Outcome != Passed && observation.Outcome != Failed && observation.Outcome != Inconclusive {
		return errors.New("adapter returned an invalid observation outcome")
	}
	if observation.Outcome != Passed {
		return nil
	}
	if observation.Detail == "" || len(observation.Artifacts) == 0 {
		return errors.New("passed boundary requires observed detail and raw evidence references")
	}
	for _, artifact := range observation.Artifacts {
		if artifact.Path == "" || artifact.Role == "" || artifact.SHA256 == "" {
			return errors.New("passed boundary evidence requires path, role and immutable SHA-256 digest")
		}
	}
	if kind == FixtureComparison {
		isReport := false
		for _, artifact := range observation.Artifacts {
			if artifact.Role == "scenario_comparison" {
				isReport = true
			}
		}
		if !isReport {
			return errors.New("fixture comparison requires a scenario_comparison report artifact")
		}
	}
	if kind == PrepareAccepted {
		effective := observation.Effective
		if effective == nil {
			return errors.New("accepted prepare must record actual effective inputs")
		}
		if effective.Level != definition.Level || effective.Occurrence != definition.Occurrence ||
			effective.Difficulty != definition.Difficulty || effective.MapSeed != definition.MapSeed ||
			effective.PrepareMask != definition.PrepareMask {
			return errors.New("effective prepare inputs differ from requested scenario")
		}
		if effective.PopulationProvenance == "" {
			return errors.New("effective population provenance must state known inputs or unknown ownership")
		}
	}
	return nil
}

func validateVersions(versions []Version) error {
	components := map[string]bool{"runner": false, "server": false, "launcher": false,
		"fang": false, "client": false, "content": false}
	for _, version := range versions {
		isSeen, isRequired := components[version.Component]
		if !isRequired || isSeen {
			return fmt.Errorf("unexpected or duplicate version component %q", version.Component)
		}
		if version.BuildID == "" || version.Artifact.Path == "" || version.Artifact.SHA256 == "" {
			return fmt.Errorf("actual binary/content provenance missing for %s", version.Component)
		}
		components[version.Component] = true
	}
	for _, component := range []string{"runner", "server", "launcher", "fang", "client", "content"} {
		if !components[component] {
			return fmt.Errorf("actual version missing for %s", component)
		}
	}
	return nil
}

func validateCorrelation(manifest Manifest, kind StepKind, observation Observation) error {
	if kind == Launch {
		return nil
	}
	if observation.SessionID == "" {
		return errors.New("passed boundary requires the observed authenticated session")
	}
	if kind != Authenticated && kind != PrepareAccepted && observation.ZoneGeneration == 0 {
		return errors.New("passed world boundary requires an observed zone generation")
	}
	for _, step := range manifest.Steps {
		if step.Outcome != Passed {
			continue
		}
		previous := step.Observation
		if previous.SessionID != "" && previous.SessionID != observation.SessionID {
			return errors.New("observed boundary belongs to a different authenticated session")
		}
		if previous.ZoneGeneration != 0 && previous.ZoneGeneration != observation.ZoneGeneration {
			return errors.New("observed boundary belongs to a different zone generation")
		}
	}
	return nil
}

func (e *Runner) checkpoint(ctx context.Context, manifest Manifest) error {
	checkpointContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := e.store.Save(checkpointContext, manifest)
	if err != nil {
		return fmt.Errorf("checkpointSave: %w", err)
	}
	return nil
}

func (e *Runner) captureFailure(ctx context.Context, manifest *Manifest) {
	if len(manifest.Steps) == 0 || manifest.Steps[0].StartedAt.IsZero() || e.observer == nil {
		return
	}
	// A canceled parent must not prevent bounded terminal evidence collection.
	captureContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	startedAt := time.Now().UTC()
	observation, err := e.observer.Observe(captureContext, ObservationRequest{
		RunID: manifest.RunID, Paths: manifest.Paths, Definition: manifest.Definition,
		Milestone: Milestone{Kind: TerminalFailure, Timeout: 5 * time.Second},
	})
	if err == nil {
		err = validateObservation(manifest.Definition, TerminalFailure, observation)
	}
	if err == nil && observation.Outcome == Passed {
		err = e.store.VerifyEvidence(captureContext, manifest.Paths, observation.Artifacts)
		if err != nil {
			err = fmt.Errorf("failureEvidence: %w", err)
		}
	}
	status := StepStatus{Kind: TerminalFailure, Outcome: observation.Outcome, StartedAt: startedAt,
		FinishedAt: time.Now().UTC(), Observation: observation, Detail: observation.Detail}
	if err != nil {
		status.Outcome = Inconclusive
		status.Detail = fmt.Sprintf("failureCapture: %s", err)
		status.Observation.Outcome = Inconclusive
		status.Observation.Detail = status.Detail
	}
	manifest.Steps = append(manifest.Steps, status)
	manifest.Artifacts = append(manifest.Artifacts, observation.Artifacts...)
}

func (e *Runner) finish(ctx context.Context, manifest Manifest, outcome Outcome, runErr error) (Manifest, error) {
	// Verification can create an owned worker and a suspended client. Close
	// must also handle partial verification and must be idempotent before start.
	if e.launcher != nil {
		cleanupContext, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		cleanupErr := e.launcher.Close(cleanupContext, CloseRequest{RunID: manifest.RunID, Paths: manifest.Paths})
		cleanupCancel()
		if cleanupErr != nil {
			outcome = Failed
			runErr = errors.Join(runErr, fmt.Errorf("ownedCleanup: %w", cleanupErr))
		}
	}
	manifest.Outcome = outcome
	manifest.FinishedAt = time.Now().UTC()
	manifest.Detail = "all requested boundaries and fixture comparison passed with evidence"
	if runErr != nil {
		manifest.Detail = runErr.Error()
	}
	finalContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := e.store.Save(finalContext, manifest)
	if err != nil {
		if runErr != nil {
			return manifest, fmt.Errorf("runFinalize: %w", errors.Join(runErr, err))
		}
		return manifest, fmt.Errorf("manifestFinalize: %w", err)
	}
	if runErr != nil {
		return manifest, fmt.Errorf("runFinish: %w", runErr)
	}
	return manifest, nil
}
