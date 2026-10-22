package gameplay

import zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"

func withMutationAgentDeath(snapshot zonenpc.Snapshot, definition zoneNPCDeathDefinition) zoneNPCDeathDefinition {
	if !zonenpc.HasMutationAgentDeath(snapshot.Plan) {
		return definition
	}
	// Lua393 Tick notifies a positioned explosion and marks for deletion in
	// the same callback. Its Activate and Deactivate callbacks are empty.
	definition.isImmediateDelete = true
	definition.isDeathAnimationSuppressed = true
	definition.ordinaryDeathAnimation = ""
	definition.corpseFadeDelay = 0
	definition.graphicsState = 0
	definition.explosionEffectName = "mutation_agent_explosion.ServerEventDef"
	definition.deleteDelay = 0
	return definition
}
