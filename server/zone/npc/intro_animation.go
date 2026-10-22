package npc

import "time"

// IntroductionAnimation reports the actor-owned animation phase. Its elapsed
// time is independent of the transport clock and survives checkpoint rebasing.
func (e Snapshot) IntroductionAnimation(now time.Time) (string, time.Duration, bool) {
	intro := e.introduction
	if now.IsZero() || e.IsDefeated || !e.IsPublished || e.Plan.OwnerObjectID != 0 ||
		e.Plan.IsIntroductionHidden || !e.IsIntroductionPending() || !intro.isCommitted ||
		intro.profile.FirstAggroAnimationName == "" {
		return "", 0, false
	}
	if intro.profile.FirstAggroAnimationDelay > 0 && !intro.isAnimationEmitted {
		return "", 0, false
	}
	delay := max(intro.profile.FirstAggroRevealDelay, intro.profile.FirstAggroAnimationDelay)
	elapsed := now.Sub(intro.startedAt)
	if elapsed < delay {
		return "", 0, false
	}
	return intro.profile.FirstAggroAnimationName, elapsed - delay, true
}
