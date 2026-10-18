package boss

import (
	"errors"
	"strings"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// CanRevive keeps delayed resurrection from reopening a completed arena.
// Actors outside this encounter retain their ordinary resurrection rules.
func (e *Session) CanRevive(snapshot zonenpc.Snapshot) bool {
	if e == nil {
		return true
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.isRosterMember(snapshot.Plan) {
		return true
	}
	return e.phase == PhaseActive || e.phase == PhaseArming
}

// Revive restores the same admitted object to the live wave; it does not
// create another spawn, repeat the opening wave or reset captain admission.
func (e *Session) Revive(snapshot zonenpc.Snapshot) error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.isRosterMember(snapshot.Plan) {
		return nil
	}
	if snapshot.IsDefeated || snapshot.HitPoint <= 0 ||
		(e.phase != PhaseActive && e.phase != PhaseArming) {
		return errors.New("boss resurrection unavailable")
	}
	e.liveObjectIDs[snapshot.Plan.ObjectID] = true
	if snapshot.Plan.ObjectID == e.leaderObjectID {
		e.leaderHitPoint = snapshot.HitPoint
	}
	return nil
}

// The caller holds the session lock. Marker identity alone is insufficient:
// scripted summons can share it without belonging to the boss wave.
func (e *Session) isRosterMember(plan zonenpc.SpawnPlan) bool {
	if plan.ObjectID == 0 || !strings.EqualFold(plan.MarkerSetName, e.publication.MarkerSetName) {
		return false
	}
	for _, admitted := range e.plans {
		if admitted.ObjectID == plan.ObjectID {
			return true
		}
	}
	return false
}
