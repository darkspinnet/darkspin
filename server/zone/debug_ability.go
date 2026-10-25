//go:build scenario

package zone

import zonehero "github.com/darkspinnet/darkspin/server/zone/hero"

func (e *Zone) ScenarioAbilityActor(userID, generation uint64) (uint64, zonehero.Actor, bool) {
	if e == nil || !e.mu.TryRLock() {
		return 0, zonehero.Actor{}, false
	}
	defer e.mu.RUnlock()
	member, isFound := e.members[userID]
	if !isFound || member.PeerGeneration != generation || e.info.Hero == nil {
		return 0, zonehero.Actor{}, false
	}
	actor, isActor := e.info.Hero.ScenarioActor(userID, generation)
	return e.generation, actor, isActor
}
