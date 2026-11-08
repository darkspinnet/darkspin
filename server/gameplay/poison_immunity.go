package gameplay

import (
	"errors"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

// The caller holds registry.mutex. This reads the actual recipient's own live
// logical80; attached damage snapshots and owner mirrors are not live grants.
func (e campaignNPCActionRuntime) poisonAdmissionLocked(member gameplayPeerSession, objectID uint32, now time.Time) (bool, error) {
	world := member.zone
	if world == nil {
		return false, errors.New("poison world unavailable")
	}
	for _, target := range e.registry.sessions {
		if target.zone != world || target.deployedObjectID != objectID {
			continue
		}
		if target.deployedHitPoint() <= 0 || int(target.deployedCreatureIndex) >= len(target.binding.Creatures) {
			return false, nil
		}
		ownImmunity, isKnown := target.heroOwnImmunity(now)
		if !isKnown {
			return false, errors.New("poison hero immunity unavailable")
		}
		// Native9E16C0's general IsDebuff gate is independent of DOT149's
		// Chain-only logical80 gate. Overdrive grants own80, not general49.
		if ownImmunity.isDebuffImmune {
			return false, nil
		}
		return target.binding.Mode != game.ModeChain || ownImmunity.poisonOrDisease <= 0, nil
	}
	immunity, isKnown := world.NPCs().PoisonImmunity(objectID)
	if !isKnown {
		immunity, isKnown = world.Companion().PoisonImmunity(objectID)
	}
	if !isKnown {
		return false, errors.New("poison recipient own immunity unavailable")
	}
	return member.binding.Mode != game.ModeChain || immunity <= 0, nil
}
