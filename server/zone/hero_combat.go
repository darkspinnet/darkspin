package zone

import zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"

type HeroCombatState struct {
	ObjectID      uint32
	IsInCombat    bool
	AttackerCount uint32
}

// HeroCombatStates exposes current enemy targets to the hero's idle animation
// and audio controllers. Counts belong to each hero, not their whole party or
// companions. Recompute from live targets because the threat ledger can retain
// entries after retargeting or death. Fixtures and unpublished actors do not count.
// Build 103's sub_4D9710 selects victoryIdleAnimState during the first five
// stationary seconds after its agent leaves combat; the client owns playback.
func (e *Zone) HeroCombatStates() []HeroCombatState {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.state != StateActive || e.info.Hero == nil {
		return nil
	}
	attackerCountsByObjectID := make(map[uint32]uint32)
	for _, npc := range e.info.NPCs.LiveSnapshots() {
		if !npc.IsPublished || npc.Plan.IsFixture || npc.HitPoint <= 0 ||
			npc.Faction != zonenpc.FactionNonPlayerAligned || npc.TargetObjectID == 0 {
			continue
		}
		attackerCountsByObjectID[npc.TargetObjectID]++
	}
	actors := e.info.Hero.Snapshots()
	states := make([]HeroCombatState, 0, len(actors))
	for _, actor := range actors {
		attackerCount := uint32(0)
		if actor.HitPoint > 0 {
			attackerCount = attackerCountsByObjectID[actor.ObjectID]
		}
		states = append(states, HeroCombatState{
			ObjectID: actor.ObjectID, IsInCombat: attackerCount > 0,
			AttackerCount: attackerCount,
		})
	}
	return states
}
