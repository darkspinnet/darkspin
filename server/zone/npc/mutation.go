package npc

import "time"

const (
	MutationAgentNounName          = "MutationAgent.Noun"
	MutationAgentTransformDuration = time.Second
)

// MutationAgentActionProfile keeps an explicitly spawned developer actor
// stationary. The developer spawn adapter supplies a renderable body because
// MutationAgent.Noun has no render record, while this attached hostile aura
// preserves its packaged presentation.
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
