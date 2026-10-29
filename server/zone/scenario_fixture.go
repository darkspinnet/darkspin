//go:build scenario

package zone

import zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"

// The NPC session remains the single membership owner. Retirement removes a
// controlled fixture from both hazard discovery and rejoin fixture delivery.
func (e *Zone) scenarioFixturePlans() []zonenpc.SpawnPlan {
	plans := make([]zonenpc.SpawnPlan, 0)
	for _, actor := range e.info.NPCs.Snapshots() {
		if actor.Plan.IsFixture && actor.IsScenarioControlled() && actor.IsPublished && !actor.IsDefeated {
			plans = append(plans, actor.Plan.Clone())
		}
	}
	return plans
}
