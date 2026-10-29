package npc

import (
	"strings"
	"time"
)

// Build103 definitions2376044295/3871933348 bind the same living aura and
// immediate death callback, while retaining their different installed classes.
func IsCryosFungus(plan SpawnPlan) bool {
	return plan.IsFixture && (strings.EqualFold(plan.NounName, "DEST_prefab_cryos_plants_shascope.Noun") || strings.EqualFold(plan.NounName, "DEST_cryos_plants_shascope.Noun"))
}

func MushroomPoisonProfile() ActionProfile {
	return ActionProfile{AbilityName: "MushroomCryosPoison", ModifierName: "MushroomCryosPoison",
		ModifierDuration: 4 * time.Second, ModifierTickDuration: 4 * time.Second, ModifierMaximumStack: 5,
		ModifierMinimumTickDamage: 15, ModifierMaximumTickDamage: 20, ModifierTickDamageCoefficient: .05,
		ModifierDamageType: 2, ModifierDamageSource: 1, ModifierDescriptorMask: 1<<2 | 1<<5 | 1<<14,
		IsModifierDamageProfileKnown: true, IsDamageProfileKnown: true}
}
