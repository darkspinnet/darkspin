package npc

import "time"

// introductionTimeline belongs to the actor. Losing its action driver does not
// restart the authored entrance or shorten the remaining animation wait.
type introductionTimeline struct {
	startedAt         time.Time
	profile           ActionProfile
	timestamp         uint64
	generation        uint64
	isCommitted       bool
	isComplete        bool
	isEffectEmitted   bool
	isBossActive      bool
	isInitiallyHidden bool
}

// IntroductionTiming contains remaining waits, never new authored durations.
type IntroductionTiming struct {
	Delay           time.Duration
	RevealDelay     time.Duration
	EffectDelay     time.Duration
	ActiveDelay     time.Duration
	Timestamp       uint64
	Profile         ActionProfile
	IsNew           bool
	IsRevealPending bool
	IsEffectPending bool
	IsActivePending bool
	Generation      uint64
}

func (e Snapshot) IsIntroductionPending() bool {
	return !e.introduction.startedAt.IsZero() && !e.introduction.isComplete
}

func (e Snapshot) IsIntroductionBossActive() bool {
	return e.introduction.startedAt.IsZero() || e.introduction.isBossActive
}

// StartIntroduction admits or resumes the same actor-owned cinematic timeline.
func (e *Session) StartIntroduction(objectID uint32, owner ActionOwner, generation uint64,
	profile ActionProfile, now time.Time, timestamp uint64,
) (IntroductionTiming, bool) {
	if e == nil || now.IsZero() || !profile.IsFirstAggroDurationKnown || profile.FirstAggroCinematicDuration <= 0 {
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
		npc.introduction = introductionTimeline{
			startedAt: now, profile: profile, timestamp: timestamp, generation: generation,
			isInitiallyHidden: npc.Plan.IsIntroductionHidden,
		}
	}
	intro := npc.introduction
	elapsed := max(now.Sub(intro.startedAt), 0)
	activeDelay := intro.profile.FirstAggroDelay
	if intro.profile.FirstAggroRevealDelay > 0 {
		activeDelay = intro.profile.FirstAggroRevealDelay
	}
	timing := IntroductionTiming{
		Delay:       max(intro.profile.FirstAggroDelay-elapsed, 0),
		RevealDelay: max(intro.profile.FirstAggroRevealDelay-elapsed, 0),
		EffectDelay: max(intro.profile.FirstAggroEffectDelay-elapsed, 0),
		ActiveDelay: max(activeDelay-elapsed, 0),
		Timestamp:   intro.timestamp, Profile: intro.profile, IsNew: isNew, Generation: intro.generation,
		IsRevealPending: npc.Plan.IsIntroductionHidden && intro.profile.FirstAggroRevealDelay > 0,
		IsEffectPending: !intro.isEffectEmitted && intro.profile.FirstAggroEffectName != "" && intro.profile.FirstAggroEffectDelay > 0,
		IsActivePending: !intro.isBossActive,
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
	npc.Plan.IsIntroductionComplete = false
	npc.IsInvisibleToSecurityTeleporter = npc.Plan.IsIntroductionHidden
	npc.IsFirstActionStarted = false
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
