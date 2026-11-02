package npc

import "strings"

// The authored graph identifies the script even when a developer projection
// uses another renderable body noun for the Mutation Agent.
func HasMutationAgentDeath(plan SpawnPlan) bool {
	graph := plan.NPCProfile.AIGraph
	return graph != nil && graph.IsResolved && graph.DeathAbility != nil &&
		strings.EqualFold(*graph.DeathAbility, "MutationAgentDeath")
}

// FinishMutationAgentDeath applies MarkForDelete to this exact dead life.
// Source snapshots and checkpoint state never turn into a repairable corpse.
func (e *Session) FinishMutationAgentDeath(source Snapshot) bool {
	if e == nil || !HasMutationAgentDeath(source.Plan) {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	current, isFound := e.npcs[source.Plan.ObjectID]
	if !isFound || !current.IsDefeated || current.HitPoint > 0 ||
		current.Mutation.Generation != source.Mutation.Generation || current.Plan.NounName != source.Plan.NounName {
		return false
	}
	current.IsPublished = false
	e.npcs[source.Plan.ObjectID] = current
	return true
}
