package npc

import "time"

// NashiraCurseProfile uses build-103 chunks 604 and 649. The phase admits
// this cast only for the two higher noun tiers; rank selects Lua properties.
func NashiraCurseProfile(plan SpawnPlan) (ActionProfile, bool) {
	if shadowBossRank(plan.NounName) < 2 {
		return ActionProfile{}, false
	}
	rank := EffectiveNPCRank(plan)
	cooldowns := [...]time.Duration{10 * time.Second, 10 * time.Second, 6 * time.Second}
	ranges := [...]float32{18, 21, 24}
	minimumDamage := float32(3)
	maximumDamage := float32(6)
	if rank >= 2 {
		// GetRankedValue clamps the two-entry modifier table at its last pair.
		minimumDamage, maximumDamage = 6, 12
	}
	return ActionProfile{
		Family: ActionRetainedArea, AbilityName: "CastShadowCurse",
		AnimationName: "cast_shadowboss_curse",
		HitDelay:      366666675 * time.Nanosecond,
		ReleaseDelay:  1633332968 * time.Nanosecond,
		Cooldown:      cooldowns[rank-1], Range: ranges[rank-1],
		MovementSpeed: nashiraMovementSpeed, NonCombatMovementSpeed: nashiraNonCombatSpeed,
		DescriptorMask: 1 << 7, DamageType: 3, DamageSource: 1,
		ModifierName: "ShadowCurse", ModifierDuration: 6 * time.Second,
		ModifierTickDuration:      2 * time.Second,
		ModifierMinimumTickDamage: minimumDamage, ModifierMaximumTickDamage: maximumDamage,
		ModifierTickDamageCoefficient: .05, ModifierMaximumStack: 1,
		ModifierDescriptorMask: 1<<2 | 1<<5 | 1<<7, ModifierDamageType: 4, ModifierDamageSource: 1,
		IsModifierDamageProfileKnown: true, IsDamageProfileKnown: true,
		TargetEffectName:          "status_cursed.ServerEventDef",
		IsFirstAggroDurationKnown: true,
	}, true
}
