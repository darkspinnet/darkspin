package npc

import (
	"strings"
	"time"
)

// NomadSpecialFourProfile is the parent selector from build-103 chunk860.
// Its child action is selected after this one-second chargeup, not on admission.
func NomadSpecialFourProfile(nounName string) (ActionProfile, bool) {
	name := strings.ToLower(nounName)
	if name != "nomadspecialfour.noun" && name != "nomadspecialfour_2.noun" && name != "nomadspecialfour_3.noun" {
		return ActionProfile{}, false
	}
	return ActionProfile{Family: ActionMelee, AbilityName: "NomadSpecialFour",
		AnimationName: "nomad_lieu_lf_4_chargeup_d3", Range: 15,
		HitDelay: time.Second, ReleaseDelay: time.Second, Cooldown: 6 * time.Second,
		MovementSpeed: 5.5, NonCombatMovementSpeed: 4,
		IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true}, true
}

func NomadSpecialFourChildProfile(rank int32, isToss bool) ActionProfile {
	rank = max(int32(1), min(int32(3), rank))
	profile := ActionProfile{Family: ActionMelee, AbilityName: "NomadSpecialFourVomit",
		AbilityRank: rank, AnimationName: "nomad_lieu_lf_4_attack2", Range: 50,
		HitDelay: 233333334 * time.Nanosecond, ReleaseDelay: 1399999976 * time.Nanosecond,
		Cooldown: NomadSpecialFourCooldown(rank), IsFacingPolicyKnown: true,
		IsFacingSuppressed: true, Radius: 2,
		ModifierName: "NomadSpecialFour_CageModifier", ModifierDuration: 7 * time.Second,
		MovementSpeed: 5.5, IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true}
	if !isToss {
		return profile
	}
	profile.Family, profile.AbilityName = ActionProjectile, "NomadSpecialFourToss"
	profile.AnimationName = "nomad_lieu_lf_4_attack1"
	profile.NearAnimationName, profile.FarAnimationName = profile.AnimationName, profile.AnimationName
	profile.Range = [...]float32{12, 14, 16}[rank-1]
	profile.Radius, profile.ProjectileSpeed, profile.ProjectileFlightDuration = .5, 25, time.Second
	profile.ProjectileNoun = "Ability_Toss.Noun"
	profile.TrailEffectName = "nomad_lieu_lf_4_goop_projectile.ServerEventDef"
	profile.ImpactEffectName = "nomad_lieu_lf_4_goop_AOE_hit.ServerEventDef"
	return profile
}

func NomadSpecialFourCooldown(rank int32) time.Duration {
	return [...]time.Duration{6 * time.Second, 5 * time.Second, 4199999809 * time.Nanosecond}[max(int32(1), min(int32(3), rank))-1]
}

// The retained pool applies chunk9's UniqueIrreplaceable poison with its own
// object as initiator and the source's copied attribute snapshot.
func NomadSpecialFourPoisonProfile(rank int32) ActionProfile {
	rank = max(int32(1), min(int32(3), rank))
	return ActionProfile{AbilityName: "NomadSpecialFourPoison", AbilityRank: rank,
		ModifierName: "NomadSpecialFour_Poison", ModifierDuration: [...]time.Duration{6 * time.Second, 8 * time.Second, 10 * time.Second}[rank-1],
		ModifierTickDuration: time.Second, ModifierMaximumStack: 1,
		ModifierMinimumTickDamage: [...]float32{4, 6, 10}[rank-1], ModifierMaximumTickDamage: [...]float32{8, 12, 16}[rank-1],
		ModifierTickDamageCoefficient: .05, ModifierDescriptorMask: 1<<2 | 1<<5,
		ModifierDamageType: 2, ModifierDamageSource: 1, IsModifierDamageProfileKnown: true,
		IsDamageProfileKnown: true}
}
