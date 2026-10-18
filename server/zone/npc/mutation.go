package npc

import "time"

const (
	MutationAgentNounName          = "MutationAgent.Noun"
	MutationAgentTransformDuration = time.Second
)

// MutationAgentActionProfile supplies the agent's passive aura. Horde agents
// receive their packaged smoke model from the spawn projection because the
// noun has no ordinary render record.
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
