package gameplay

import "log"

// Pursuit has several early cancellation paths. Retire feature-owned movement
// after those paths release the registry lock, including direct NPC lease drops.
func (e campaignNPCPursuitRuntime) retireInterruptedMovement(sessionKey string, generation uint64, objectID uint32, actionGeneration uint64) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil {
		return
	}
	actor, isActorFound := member.zone.NPCs().NPC(objectID)
	if isActorFound && !actor.IsDefeated && actor.IsActionStarted && actor.ActionGeneration == actionGeneration {
		return
	}
	blinkErr := member.retireSpacetimeBlinkLocked(objectID, actionGeneration)
	if blinkErr != nil {
		log.Printf("NPC interrupted blink cleanup failed object=%d: %v", objectID, blinkErr)
	}
	moveErr := member.retireAdditionalKitMovesLocked(objectID, actionGeneration)
	if moveErr != nil {
		log.Printf("NPC interrupted movement cleanup failed object=%d: %v", objectID, moveErr)
	}
	e.registry.sessions[sessionKey] = member
}
