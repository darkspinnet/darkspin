package npc

import "strings"

func IsFactoryLight(nounName string) bool {
	return strings.EqualFold(nounName, "DEST_prefab_citadel_factorylight.Noun")
}

func IsMutationEgg(nounName string) bool {
	return strings.EqualFold(nounName, "DEST_mutationagent.Noun")
}

// Build 103 chunk906 is distinct from the Horde MutationAgent callback393.
func MutationEggDeathProfile() ActionProfile {
	return ActionProfile{Family: ActionRetainedArea, AbilityName: "NocturnaEggDeath",
		Radius: 5, MinimumDamage: 15, MaximumDamage: 15,
		DamageType: 3, DamageSource: 1, DescriptorMask: 1<<3 | 1<<7,
		IsDamageProfileKnown: true}
}

func (e *Session) FinishMutationEggDeath(source Snapshot) bool {
	if e == nil || !IsMutationEgg(source.Plan.NounName) {
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
