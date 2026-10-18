package companion

import (
	"log"

	"github.com/darkspinnet/darkspin/server/game"
)

// ConfigureActorFootprints supplies native noun geometry without replacing
// the physics radius retained by companion projectile and presentation paths.
func (e *Session) ConfigureActorFootprints(footprints map[uint32]game.NavigationFootprint) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.actorFootprints = footprints
	for objectID, actor := range e.actors {
		footprint, isFound := footprints[actor.Noun]
		if isFound {
			actor.ActorFootprint = &footprint
			e.actors[objectID] = actor
		}
	}
}

func (e Actor) ActorFootprintRadius() float32 {
	if e.ActorFootprint == nil {
		return e.FootprintRadius
	}
	radius, err := e.ActorFootprint.ActorRadius(1)
	if err != nil {
		log.Printf("companion actor footprint noun=%#x: %v", e.Noun, err)
		return 0
	}
	return radius
}
