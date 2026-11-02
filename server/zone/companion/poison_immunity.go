package companion

// Logical80 has native initialized own base zero and is not owner-mirrored.
// Current companion spawn/buff/enrage producers do not grant logical80; a
// future own grant must update this actor state, never the owner's snapshot.
func (e *Session) PoisonImmunity(objectID uint32) (float32, bool) {
	if e == nil {
		return 0, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, isFound := e.actors[objectID]
	if !isFound {
		return 0, false
	}
	return actor.poisonImmunity, true
}
