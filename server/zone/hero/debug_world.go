//go:build scenario

package hero

// ScenarioActor takes no waiting lock while a gameplay registry observation is
// in progress. A busy producer is unavailable rather than blocking admission.
func (e *Session) ScenarioActor(userID, generation uint64) (Actor, bool) {
	if e == nil || !e.mu.TryRLock() {
		return Actor{}, false
	}
	defer e.mu.RUnlock()
	actor, isFound := e.actors[userID]
	return actor, isFound && actor.PeerGeneration == generation
}
