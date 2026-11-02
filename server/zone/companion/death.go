package companion

import (
	"time"

	"github.com/darkspinnet/darkspin/server/util"
)

// DeathProfile contains only the two installed, recovered pet death scripts.
// Their WaitUntilTime calls have no Basic descriptor or optional speed scaling.
type DeathProfile struct {
	AbilityName   string
	AnimationName string
	Duration      time.Duration
}

func DeathProfileForNoun(noun uint32) (DeathProfile, bool) {
	switch noun {
	case util.HashID("RangedElementalPet.Noun"):
		return DeathProfile{AbilityName: "FireElementalPetDeath", AnimationName: "firetempest_pet_death", Duration: 1399999976 * time.Nanosecond}, true
	case util.HashID("PlasmaSentinelPet.Noun"):
		return DeathProfile{AbilityName: "PlasmaSentinelPetDeath", AnimationName: "el_plasmasentinel_pet_death", Duration: 1899999976 * time.Nanosecond}, true
	default:
		return DeathProfile{}, false
	}
}

// BeginDeath keeps the corpse identity while preventing target/follow/attack
// admission. The committed HP transition already stopped its locomotion.
func (e *Session) BeginDeath(expected Actor) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[expected.ObjectID]
	if !isFound || !isSameDeath(actor, expected) {
		return false
	}
	actor.IsTargetable = false
	actor.IsAbilityControlled = true
	actor.TargetObjectID = 0
	actor.clearFollow()
	actor.clearPursuit()
	e.actors[actor.ObjectID] = actor
	return true
}

// FinishDeath cannot remove a replacement or resurrected companion.
func (e *Session) FinishDeath(expected Actor) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[expected.ObjectID]
	if !isFound || !isSameDeath(actor, expected) {
		return false
	}
	delete(e.actors, actor.ObjectID)
	e.motionRevision++
	return true
}

func isSameDeath(actor Actor, expected Actor) bool {
	return actor.ObjectID == expected.ObjectID && actor.Noun == expected.Noun &&
		actor.UserID == expected.UserID && actor.PeerGeneration == expected.PeerGeneration &&
		actor.OwnerObjectID == expected.OwnerObjectID && actor.DeathRevision == expected.DeathRevision &&
		actor.HitPoint == 0 && expected.HitPoint == 0 && actor.DeathRevision != 0
}
