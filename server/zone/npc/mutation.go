package npc

import "time"

const (
	MutationAgentNounName          = "MutationAgent.Noun"
	MutationAgentTransformDuration = time.Second
	mutationBeamHitDelay           = 1700000047 * time.Nanosecond
	mutationAuraHitDelay           = 150000005 * time.Nanosecond
)

// MutationAgentActionProfile identifies the authored passive to content
// projection. The mutation lifecycle owns its effects and actions.
func MutationAgentActionProfile() ActionProfile {
	return ActionProfile{
		Family: ActionUnknown, AbilityName: "MutationAgentPassive",
		Range: 1, MovementSpeed: 5.95, NonCombatMovementSpeed: 3.9666667,
		IsSelfTargeted: true,
		HitDelay:       MutationAgentTransformDuration,
		ReleaseDelay:   MutationAgentTransformDuration,
		Cooldown:       2 * time.Second,
	}
}
