package gameplay

import (
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

// Build 103 applies recipient attribute 30 immediately before each health
// gain. Read the active Arena modifier at that point, including healing from
// another player or a producer that outlives its original caster.
func (e gameplayPeerSession) healingTargetProfile(creatureIndex uint32) game.HealingTargetProfile {
	profile := e.binding.Creatures[creatureIndex].HealingTargetProfile
	if e.binding.Mode != game.ModeArena || creatureIndex != e.deployedCreatureIndex {
		return profile
	}
	if e.isArenaHealingReductionActive(time.Now()) {
		profile.HealingReduction += heroHealingReduction
	}
	return profile
}

func (e gameplayPeerSession) isArenaHealingReductionActive(at time.Time) bool {
	for _, status := range e.arenaHitStatuses {
		if status != nil && status.kind == arenaBasicHealingReduction && status.matches(e) && at.Before(status.expiresAt) {
			return true
		}
	}
	return false
}
