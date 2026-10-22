package npc

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

// IntroductionState is process-independent entrance progress. Elapsed freezes
// at capture, matching the zone checkpoint's paused mission clock. Source
// packet timestamps and action-driver ownership never cross process restarts.
type IntroductionState struct {
	Elapsed            time.Duration
	SourcePosition     game.Vec3
	Profile            ActionProfile
	Generation         uint64
	IsSet              bool
	IsCommitted        bool
	IsComplete         bool
	IsEffectEmitted    bool
	IsAnimationEmitted bool
	IsBossActive       bool
	IsInitiallyHidden  bool
}

func (e Snapshot) CheckpointIntroduction(at time.Time) (IntroductionState, error) {
	intro := e.introduction
	if intro.startedAt.IsZero() {
		return IntroductionState{}, nil
	}
	if at.IsZero() {
		return IntroductionState{}, errors.New("introduction capture time unavailable")
	}
	state := IntroductionState{
		Elapsed:        max(at.Sub(intro.startedAt), 0),
		SourcePosition: intro.sourcePosition,
		Profile:        WithRetainedIntroduction(ActionProfile{}, IntroductionTiming{Profile: intro.profile}),
		Generation:     intro.generation, IsSet: true, IsCommitted: intro.isCommitted,
		IsComplete: intro.isComplete, IsEffectEmitted: intro.isEffectEmitted,
		IsAnimationEmitted: intro.isAnimationEmitted,
		IsBossActive:       intro.isBossActive, IsInitiallyHidden: intro.isInitiallyHidden,
	}
	err := ValidateIntroductionState(e.Plan, state)
	if err != nil {
		return IntroductionState{}, fmt.Errorf("introCapture: %w", err)
	}
	return state, nil
}

func ValidateIntroductionState(plan SpawnPlan, state IntroductionState) error {
	if !state.IsSet {
		return nil
	}
	if state.Elapsed < 0 || state.Generation == 0 || !state.IsCommitted ||
		!zonegeometry.IsFinite(state.SourcePosition) ||
		!CanRetainIntroduction(plan, state.Profile) {
		return errors.New("invalid introduction checkpoint")
	}
	profile := state.Profile
	if state.IsAnimationEmitted && (profile.FirstAggroAnimationDelay <= 0 ||
		state.Elapsed < max(profile.FirstAggroAnimationDelay, profile.FirstAggroRevealDelay)) {
		return errors.New("invalid introduction animation progress")
	}
	if profile.FirstAggroDelay < 0 || profile.FirstAggroRevealDelay < 0 ||
		profile.FirstAggroAnimationDelay < 0 ||
		profile.FirstAggroEffectDelay < 0 || profile.FirstAggroCinematicDuration < 0 ||
		!zonegeometry.IsFinite(profile.FirstAggroCinematicOffset) ||
		profile.FirstAggroCinematicRadius < 0 ||
		math.IsNaN(float64(profile.FirstAggroCinematicRadius)) ||
		math.IsInf(float64(profile.FirstAggroCinematicRadius), 0) {
		return errors.New("invalid introduction checkpoint timing")
	}
	return nil
}

// WithCheckpointIntroduction resumes only the saved remaining timeline. Stages
// already accepted remain accepted, even if the process was offline for days.
func (e Snapshot) WithCheckpointIntroduction(state IntroductionState, at time.Time) (Snapshot, error) {
	err := ValidateIntroductionState(e.Plan, state)
	if err != nil {
		return Snapshot{}, fmt.Errorf("introRestore: %w", err)
	}
	if !state.IsSet {
		e.introduction = introductionTimeline{}
		return e, nil
	}
	if at.IsZero() {
		return Snapshot{}, errors.New("introduction restore time unavailable")
	}
	e.introduction = introductionTimeline{
		startedAt: at.Add(-state.Elapsed), profile: state.Profile,
		sourcePosition: state.SourcePosition,
		generation:     state.Generation, isCommitted: state.IsCommitted,
		isComplete: state.IsComplete, isEffectEmitted: state.IsEffectEmitted,
		isAnimationEmitted: state.IsAnimationEmitted,
		isBossActive:       state.IsBossActive, isInitiallyHidden: state.IsInitiallyHidden,
		isTimestampPending: true,
	}
	return e, nil
}
