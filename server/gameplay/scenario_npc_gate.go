//go:build scenario

package gameplay

import zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"

func scenarioAutonomousNPCPlans(session *zonenpc.Session, plans []zonenpc.SpawnPlan) []zonenpc.SpawnPlan {
	results := make([]zonenpc.SpawnPlan, 0, len(plans))
	for _, plan := range plans {
		actor, isFound := session.NPC(plan.ObjectID)
		if isFound && actor.IsScenarioAIEnabled() {
			results = append(results, plan)
		}
	}
	return results
}

// The first ordinary action cycle settles a manual cast. It must not resume
// the autonomous controller while its spawned actor's AI is disabled.
func (e campaignNPCActionRuntime) finishScenarioNPCManualAction(sessionKey string, generation uint64, objectID uint32, actionGeneration uint64) bool {
	e.registry.mutex.RLock()
	defer e.registry.mutex.RUnlock()
	peer, isFound := e.registry.sessions[sessionKey]
	if !isFound || peer.generation != generation || peer.zone == nil {
		return false
	}
	actor, isFound := peer.zone.NPCs().NPC(objectID)
	if !isFound || !actor.IsScenarioControlled() || actor.IsScenarioAIEnabled() {
		return false
	}
	peer.zone.NPCs().ReleaseActionGeneration(objectID, actor.ActionOwner, actionGeneration)
	return true
}
