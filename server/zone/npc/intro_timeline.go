package npc

import (
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

// introductionTimeline belongs to the actor. Losing its action driver does not
// restart the authored entrance or shorten the remaining animation wait.
type introductionTimeline struct {
	startedAt          time.Time
	sourcePosition     game.Vec3
	profile            ActionProfile
	timestamp          uint64
	generation         uint64
	isCommitted        bool
	isComplete         bool
	isEffectEmitted    bool
	isAnimationEmitted bool
	isBossActive       bool
	isInitiallyHidden  bool
	isTimestampPending bool
}

// IntroductionTiming contains remaining waits, never new authored durations.
type IntroductionTiming struct {
	Delay              time.Duration
	RevealDelay        time.Duration
	EffectDelay        time.Duration
	AnimationDelay     time.Duration
	ActiveDelay        time.Duration
	Timestamp          uint64
	Profile            ActionProfile
	IsNew              bool
	IsRevealPending    bool
	IsEffectPending    bool
	IsAnimationPending bool
	IsActivePending    bool
	Generation         uint64
}

func (e Snapshot) IsIntroductionPending() bool {
	return !e.introduction.startedAt.IsZero() && !e.introduction.isComplete
}

func (e Snapshot) IsIntroductionBossActive() bool {
	return e.introduction.startedAt.IsZero() || e.introduction.isBossActive
}

// CanRetainIntroduction admits recovered, timed entrances independently of
// whether they also request a camera hold. Floor warps and owned summons keep
// their separate spawn lifecycles.
func CanRetainIntroduction(plan SpawnPlan, profile ActionProfile) bool {
	if ((plan.OwnerObjectID != 0 || plan.Introduction == SpawnIntroductionFloorWarp) && !plan.IsSubsequentAggroPending) ||
		!profile.IsFirstAggroDurationKnown {
		return false
	}
	return profile.FirstAggroCinematicDuration > 0 ||
		(profile.FirstAggroDelay > 0 && profile.FirstAggroAnimationName != "")
}

// WithRetainedIntroduction restores entrance fields after a takeover selects
// its current combat attack. A visible actor's ordinary entrance profile may
// already have been omitted by IntroductionProfile.
func WithRetainedIntroduction(profile ActionProfile, timing IntroductionTiming) ActionProfile {
	intro := timing.Profile
	profile.FirstAggroAbilityName = intro.FirstAggroAbilityName
	profile.FirstAggroAnimationName = intro.FirstAggroAnimationName
	profile.FirstAggroAnimationDelay = intro.FirstAggroAnimationDelay
	profile.FirstAggroEffectName = intro.FirstAggroEffectName
	profile.FirstAggroEffectDelay = intro.FirstAggroEffectDelay
	profile.FirstAggroDelay = intro.FirstAggroDelay
	profile.FirstAggroRevealDelay = intro.FirstAggroRevealDelay
	profile.FirstAggroCinematicDuration = intro.FirstAggroCinematicDuration
	profile.FirstAggroCinematicRadius = intro.FirstAggroCinematicRadius
	profile.FirstAggroCinematicOffset = intro.FirstAggroCinematicOffset
	profile.IsFirstAggroDurationKnown = intro.IsFirstAggroDurationKnown
	profile.IsFirstAggroFacingSuppressed = intro.IsFirstAggroFacingSuppressed
	profile.PreAggroAnimationName = intro.PreAggroAnimationName
	return profile
}

func (e Snapshot) IsIntroductionCombatSuppressed() bool {
	return e.IsIntroductionPending() && (e.introduction.profile.FirstAggroRevealDelay > 0 ||
		e.introduction.profile.FirstAggroCinematicDuration > 0)
}

// IntroductionCombatDelay keeps independent combat controllers behind the
// actor's retained entrance, including its authored camera hold.
func (e Snapshot) IntroductionCombatDelay(now time.Time) time.Duration {
	if !e.IsIntroductionPending() {
		return 0
	}
	duration := max(e.introduction.profile.FirstAggroDelay, e.introduction.profile.FirstAggroCinematicDuration)
	return max(e.introduction.startedAt.Add(duration).Sub(now), 0)
}

// StartIntroduction admits or resumes the same actor-owned entrance timeline.
func (e *Session) StartIntroduction(objectID uint32, owner ActionOwner, generation uint64,
	profile ActionProfile, now time.Time, timestamp uint64,
) (IntroductionTiming, bool) {
	if e == nil || now.IsZero() {
		return IntroductionTiming{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isFound := e.npcs[objectID]
	if !isFound || npc.IsDefeated || !npc.IsActionStarted || npc.ActionOwner != owner || npc.ActionGeneration != generation {
		return IntroductionTiming{}, false
	}
	isNew := npc.introduction.startedAt.IsZero()
	if isNew {
		if !CanRetainIntroduction(npc.Plan, profile) {
			return IntroductionTiming{}, false
		}
		npc.introduction = introductionTimeline{
			startedAt: now, sourcePosition: npc.Plan.Position, profile: profile, timestamp: timestamp, generation: generation,
			isInitiallyHidden: npc.Plan.IsIntroductionHidden,
		}
	}
	intro := npc.introduction
	elapsed := max(now.Sub(intro.startedAt), 0)
	if intro.isTimestampPending {
		// Packet time belongs to this server process. A resumed stage uses its
		// new driver's current timestamp plus the saved remaining delay.
		intro.timestamp = timestamp
		intro.isTimestampPending = false
		npc.introduction = intro
	}
	activeDelay := intro.profile.FirstAggroDelay
	if intro.profile.FirstAggroRevealDelay > 0 {
		activeDelay = intro.profile.FirstAggroRevealDelay
	}
	timing := IntroductionTiming{
		Delay:          max(intro.profile.FirstAggroDelay-elapsed, 0),
		RevealDelay:    max(intro.profile.FirstAggroRevealDelay-elapsed, 0),
		EffectDelay:    max(intro.profile.FirstAggroEffectDelay-elapsed, 0),
		AnimationDelay: max(max(intro.profile.FirstAggroAnimationDelay, intro.profile.FirstAggroRevealDelay)-elapsed, 0),
		ActiveDelay:    max(activeDelay-elapsed, 0),
		Timestamp:      intro.timestamp, Profile: intro.profile, IsNew: isNew, Generation: intro.generation,
		IsRevealPending:    npc.Plan.IsIntroductionHidden && intro.profile.FirstAggroRevealDelay > 0,
		IsEffectPending:    !intro.isEffectEmitted && intro.profile.FirstAggroEffectName != "" && intro.profile.FirstAggroEffectDelay > 0,
		IsAnimationPending: !intro.isAnimationEmitted && intro.profile.FirstAggroAnimationName != "" && intro.profile.FirstAggroAnimationDelay > 0,
		IsActivePending:    !intro.isBossActive,
	}
	e.npcs[objectID] = npc
	return timing, true
}

// CommitIntroduction preserves a timeline after its scheduler has admitted it.
func (e *Session) CommitIntroduction(objectID uint32, generation uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isFound := e.npcs[objectID]
	if !isFound || npc.ActionGeneration != generation || npc.introduction.startedAt.IsZero() {
		return
	}
	npc.introduction.isCommitted = true
	e.npcs[objectID] = npc
}

// RejectIntroduction rolls back only an entrance whose initial schedule failed.
func (e *Session) RejectIntroduction(objectID uint32, generation uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isFound := e.npcs[objectID]
	if !isFound || npc.ActionGeneration != generation || npc.introduction.isCommitted || npc.introduction.generation != generation {
		return
	}
	npc.Plan.IsIntroductionHidden = npc.introduction.isInitiallyHidden
	if !npc.Plan.IsSubsequentAggroPending {
		npc.Plan.IsIntroductionComplete = false
		npc.IsFirstActionStarted = false
	}
	npc.IsInvisibleToSecurityTeleporter = npc.Plan.IsIntroductionHidden
	npc.introduction = introductionTimeline{}
	e.npcs[objectID] = npc
}

// CommitPublishedIntroduction records a stage only after its native packets
// commit. Combat may already have released or superseded its action by then;
// the admitted timeline identity, rather than that driver, owns this progress.
func (e *Session) CommitPublishedIntroduction(objectID uint32, generation uint64, now time.Time, stage IntroductionStage) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isFound := e.npcs[objectID]
	if !isFound || npc.IsDefeated || npc.HitPoint <= 0 || npc.introduction.generation != generation {
		return false
	}
	intro := &npc.introduction
	if intro.startedAt.IsZero() {
		return false
	}
	deadline := intro.profile.FirstAggroDelay
	switch stage {
	case IntroductionReveal:
		if !npc.Plan.IsIntroductionHidden {
			return false
		}
		deadline = intro.profile.FirstAggroRevealDelay
	case IntroductionEffect:
		if intro.isEffectEmitted {
			return false
		}
		deadline = intro.profile.FirstAggroEffectDelay
	case IntroductionAnimationStart:
		if intro.isAnimationEmitted || intro.profile.FirstAggroAnimationDelay <= 0 ||
			intro.profile.FirstAggroAnimationName == "" {
			return false
		}
		deadline = max(intro.profile.FirstAggroAnimationDelay, intro.profile.FirstAggroRevealDelay)
	case IntroductionBossActive:
		if intro.isBossActive {
			return false
		}
		if intro.profile.FirstAggroRevealDelay > 0 {
			deadline = intro.profile.FirstAggroRevealDelay
		}
	case IntroductionComplete:
		if intro.isComplete {
			return false
		}
	default:
		return false
	}
	if now.Before(intro.startedAt.Add(deadline)) {
		return false
	}
	switch stage {
	case IntroductionReveal:
		npc.Plan.IsIntroductionHidden = false
		npc.Plan.IsIntroductionComplete = true
		npc.IsInvisibleToSecurityTeleporter = false
	case IntroductionEffect:
		intro.isEffectEmitted = true
	case IntroductionAnimationStart:
		intro.isAnimationEmitted = true
	case IntroductionBossActive:
		intro.isBossActive = true
	case IntroductionComplete:
		intro.isComplete = true
	}
	e.npcs[objectID] = npc
	return true
}

type IntroductionStage uint8

const (
	IntroductionEffect IntroductionStage = iota + 1
	IntroductionBossActive
	IntroductionComplete
	IntroductionReveal
	IntroductionAnimationStart
)

// IntroductionRemaining projects an in-flight camera hold to a newly joined
// peer without replaying an animation, reveal or one-shot effect for the group.
func (e Snapshot) IntroductionRemaining(now time.Time) (time.Duration, float32) {
	if !e.IsIntroductionPending() {
		return 0, 0
	}
	duration := max(e.introduction.startedAt.Add(e.introduction.profile.FirstAggroCinematicDuration).Sub(now), 0)
	return duration, e.introduction.profile.FirstAggroCinematicRadius
}
