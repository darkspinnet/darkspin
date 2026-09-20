package zone

import zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"

type HeroCombatState struct {
	ObjectID   uint32
	IsInCombat bool
}

// HeroCombatStates exposes active enemy attention to the hero's idle animation
// controller. Fixtures, companions, dormant enemies and corpses are not threats.
// Build 103's sub_4D9710 selects victoryIdleAnimState during the first five
// stationary seconds after its agent leaves combat; the client owns playback.
func (e *Zone) HeroCombatStates() []HeroCombatState {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.state != StateActive || e.info.Hero == nil || e.info.NPCs == nil {
		return nil
	}
	threatsByObjectID := make(map[uint32]bool)
	for _, npc := range e.info.NPCs.LiveSnapshots() {
		if !npc.IsPublished || npc.Plan.IsFixture || npc.HitPoint <= 0 ||
			npc.Faction != zonenpc.FactionNonPlayerAligned || npc.TargetObjectID == 0 {
			continue
		}
		threatsByObjectID[npc.TargetObjectID] = true
	}
	actors := e.info.Hero.Snapshots()
	states := make([]HeroCombatState, 0, len(actors))
	for _, actor := range actors {
		states = append(states, HeroCombatState{
			ObjectID:   actor.ObjectID,
			IsInCombat: actor.HitPoint > 0 && threatsByObjectID[actor.ObjectID],
		})
	}
	return states
}
