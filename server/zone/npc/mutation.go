package npc

import "time"

const MutationAgentTransformDuration = time.Second

// MutationAgentActionProfile keeps the controller stationary while its
// selected escort performs the authored transform. The population planner
// retains a renderable escort body because MutationAgent.Noun has no render
// record, while this attached hostile aura preserves its packaged presentation.
func MutationAgentActionProfile() ActionProfile {
	return ActionProfile{
		Family: ActionUnknown, AbilityName: "MutationAgentPassive",
		AnimationName: "npc_mutationagent_infected_grow",
		Range:         1, MovementSpeed: 5.95, NonCombatMovementSpeed: 3.9666667,
		PassiveEffectName: "mutant_agent_aoe_smoke.ServerEventDef",
		IsSelfTargeted:    true,
		HitDelay:          MutationAgentTransformDuration,
		ReleaseDelay:      MutationAgentTransformDuration,
		Cooldown:          2 * time.Second,
	}
}
