package zone

import (
	"log"

	"github.com/darkspinnet/darkspin/server/squad"
)

// CreatureActorFootprintRadius uses the hero's noun bounds at its published
// object scale (one). Authored graphics scale belongs to the noun presentation.
// The retained physics radius is only a missing-content compatibility fallback.
func (e *Zone) CreatureActorFootprintRadius(userID, peerGeneration uint64, creatureIndex uint32) float32 {
	if e == nil || userID == 0 || peerGeneration == 0 || creatureIndex >= squad.Size {
		return 0
	}
	e.mu.RLock()
	member, isFound := e.members[userID]
	e.mu.RUnlock()
	if !isFound || member.PeerGeneration != peerGeneration {
		return 0
	}
	nounID := member.Roster.Creatures[creatureIndex].Noun
	return e.actorFootprintRadius(nounID, member.CreatureFootprints[creatureIndex])
}

func (e *Zone) actorFootprintRadius(nounID uint32, fallback float32) float32 {
	footprint, isFootprintFound := e.info.DirectorDefinition.NounFootprintsByNoun[nounID]
	if !isFootprintFound {
		return fallback
	}
	radius, err := footprint.ActorRadius(1)
	if err != nil {
		log.Printf("hero actor footprint noun=%#x: %v", nounID, err)
		return 0
	}
	return radius
}
