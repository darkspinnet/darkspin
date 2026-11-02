package npc

import (
	"strings"
	"time"
)

// Build1039E2180/9E1D40 initialize own logical80 to zero. None of the
// connected NPC affix/action/inventory producers writes it. Keep this own
// runtime state separate from owner mirrors and source damage snapshots.
func (e *Session) PoisonImmunity(objectID uint32) (float32, bool) {
	if e == nil {
		return 0, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || !actor.Plan.NPCProfile.IsKnown || !actor.Plan.NPCProfile.IsClassKnown {
		return 0, false
	}
	return actor.poisonImmunity, true
}

func IsPoisonEgg(nounName string) bool {
	return strings.EqualFold(nounName, "exploding_egg.Noun")
}

// Chunk231 has a zero initial hit and an unrestricted living-object query.
func PoisonEggDeathProfile() ActionProfile {
	return ActionProfile{Family: ActionRetainedArea, AbilityName: "ExplodingEggDeath",
		Radius: 1.5, DamageType: 2, DamageSource: 1, IsDamageProfileKnown: true,
		ModifierName: "EggPoison", ModifierDuration: 6 * time.Second,
		ModifierTickDuration: 4 * time.Second, ModifierMinimumTickDamage: 3,
		ModifierMaximumTickDamage: 6, ModifierTickDamageCoefficient: 0.05,
		ModifierDamageType: 2, ModifierDamageSource: 1,
		ModifierDescriptorMask: 1<<2 | 1<<5, IsModifierDamageProfileKnown: true}
}
