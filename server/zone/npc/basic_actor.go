package npc

import (
	"strings"
	"time"
)

// These off-roster actors retain their own noun and class. Their build-103
// definitions select the same registered Bite and BurstShot used elsewhere.
func basicActorProfile(nounName string) (ActionProfile, bool) {
	switch strings.ToLower(nounName) {
	case "critter.noun":
		return biteProfile(8, 5), true
	case "gunner.noun":
		return gunnerBurstShotProfile(), true
	default:
		return ActionProfile{}, false
	}
}

// Build-103 chunk780 is shared by Critter and BabyMortar's close attack.
func biteProfile(movementSpeed float32, nonCombatMovementSpeed float32) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "Bite", AnimationName: "cast_bite",
		Range: 0.75, Radius: 1, Cooldown: 500 * time.Millisecond,
		HitDelay: 430000007 * time.Nanosecond, ReleaseDelay: 1429999948 * time.Nanosecond,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 8, MaximumDamage: 12, DamageType: 3, DamageSource: 0,
		DescriptorMask: 67, IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true,
	}
}

// Build-103 chunk866 has four launches: 1.06 seconds, then three .4-second
// intervals. Match the imported ability's microsecond-rounded Lua deadlines.
func gunnerBurstShotProfile() ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "BurstShot", AnimationName: "cast_burstshot",
		Range: 13, Cooldown: time.Second, HitDelay: 1060 * time.Millisecond,
		ReleaseDelay:  2900 * time.Millisecond,
		MovementSpeed: 5, NonCombatMovementSpeed: 3.5,
		MinimumDamage: 2, MaximumDamage: 7, DamageCoefficient: 0.05,
		DescriptorMask: 1<<7 | 1<<13, DamageType: 3, DamageSource: 1,
		ProjectileNoun: "Ability_Fireball.Noun", ProjectileDistance: 50, ProjectileSpeed: 25,
		ProjectileShotCount: 4, ProjectileShotInterval: 400 * time.Millisecond,
		TrailEffectName:      "lightning_bullet_projectile.ServerEventDef",
		ImpactEffectName:     "plasma_common_electric_hit_medium_effect.ServerEventDef",
		MissEffectName:       "ineffective_common_small.ServerEventDef",
		IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true,
	}
}
