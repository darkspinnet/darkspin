package npc

import (
	"math"
	"time"
)

type CorruptorPhase uint8

const (
	CorruptorPhaseUnknown CorruptorPhase = iota
	CorruptorPhaseQuantum
	CorruptorPhaseNecro
	CorruptorPhasePlasma
	CorruptorPhaseLife
	CorruptorPhaseTech
)

func CorruptorPhaseEffectName(phase CorruptorPhase) string {
	switch phase {
	case CorruptorPhaseQuantum:
		return "scaldronboss_spacetime_shader_effect.ServerEventDef"
	case CorruptorPhaseNecro:
		return "scaldronboss_necro_shader_effect.ServerEventDef"
	case CorruptorPhasePlasma:
		return "scaldronboss_plasma_shader_effect.ServerEventDef"
	case CorruptorPhaseLife:
		return "scaldronboss_life_shader_effect.ServerEventDef"
	case CorruptorPhaseTech:
		return "scaldronboss_tech_shader_effect.ServerEventDef"
	default:
		return ""
	}
}

func ScaldronBossPhaseProfile(
	nounName string, phase CorruptorPhase, isStageTwo bool,
) (ActionProfile, bool) {
	switch phase {
	case CorruptorPhaseQuantum:
		profile, isFound := ScaldronBossGravityOrbProfile(nounName)
		return profile.Cast, isFound
	case CorruptorPhaseNecro:
		if isStageTwo {
			return scaldronBossShadowChargeProfile(nounName)
		}
		return scaldronBossShadowPanicProfile(nounName)
	case CorruptorPhasePlasma:
		return scaldronBossChainLightningProfile(nounName)
	case CorruptorPhaseLife:
		return scaldronBossDiseaseConeProfile(nounName)
	case CorruptorPhaseTech:
		return scaldronBossTwinLaserProfile(nounName)
	default:
		return ActionProfile{}, false
	}
}

// ScaldronBossMeleeProfile returns the packaged close-range attack paired with
// the Corruptor's currently selected elemental special.
func ScaldronBossMeleeProfile(
	nounName string, special ActionProfile,
) (ActionProfile, bool) {
	phase := CorruptorPhaseUnknown
	switch special.AbilityName {
	case "SummonGravityOrb":
		phase = CorruptorPhaseQuantum
	case "ShadowPanic", "ScaldronBoss_ShadowCharge":
		phase = CorruptorPhaseNecro
	case "ChainLightningBolt":
		phase = CorruptorPhasePlasma
	case "VerdanthBossDiseaseCone":
		phase = CorruptorPhaseLife
	case "TwinLaser":
		phase = CorruptorPhaseTech
	default:
		return ActionProfile{}, false
	}
	return scaldronBossMeleeProfile(nounName, phase)
}

func scaldronBossMeleeProfile(
	nounName string, phase CorruptorPhase,
) (ActionProfile, bool) {
	rank := scaldronBossRank(nounName)
	if rank == 0 {
		return ActionProfile{}, false
	}
	damages := [...]float32{6, 9, 12}
	profile := ActionProfile{
		Family: ActionMelee, HitDelay: 830 * time.Millisecond,
		ReleaseDelay: 1800 * time.Millisecond, Cooldown: 3 * time.Second,
		Range: 5, Radius: 7, MovementSpeed: 5, NonCombatMovementSpeed: 3,
		MinimumDamage: damages[rank-1], MaximumDamage: damages[rank-1],
		DescriptorMask: 67, DamageSource: 0,
		IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true,
	}
	switch phase {
	case CorruptorPhaseQuantum:
		profile.AbilityName = "ScaldronBoss_MeleeQuantum"
		profile.AnimationName = "sca_boss_attack_melee_sp"
		profile.DamageType = 1
		profile.ImpactEffectName = "spacetime_large_hit_effect.ServerEventDef"
	case CorruptorPhaseNecro:
		profile.AbilityName = "ScaldronBoss_MeleeNecro"
		profile.AnimationName = "sca_boss_attack_melee_su"
		profile.DamageType = 4
		profile.ImpactEffectName = "necro_common_hit_large.ServerEventDef"
	case CorruptorPhasePlasma:
		profile.AbilityName = "ScaldronBoss_MeleePlasma"
		profile.AnimationName = "sca_boss_attack_melee_el"
		profile.DamageType = 3
		profile.ImpactEffectName =
			"plasma_common_electric_hit_large_effect.ServerEventDef"
	case CorruptorPhaseLife:
		profile.AbilityName = "ScaldronBoss_MeleeBio"
		profile.AnimationName = "sca_boss_attack_melee_lf"
		profile.DamageType = 2
		profile.ImpactEffectName = "life_common_melee_hit_pc.ServerEventDef"
	case CorruptorPhaseTech:
		profile.AbilityName = "ScaldronBoss_MeleeCyber"
		profile.AnimationName = "sca_boss_attack_melee_tc"
		profile.DamageType = 0
		profile.ImpactEffectName = "cyber_common_hit_large_melee.ServerEventDef"
	default:
		return ActionProfile{}, false
	}
	return profile, true
}

func ScaldronBossGravityOrbProfile(
	nounName string,
) (GravityOrbProfile, bool) {
	rank := scaldronBossRank(nounName)
	if rank == 0 {
		return GravityOrbProfile{}, false
	}
	cooldowns := [...]time.Duration{20 * time.Second, 16 * time.Second, 12 * time.Second}
	pulls := [...]float32{2, 2.5, 3}
	lifetimes := [...]time.Duration{14 * time.Second, 12 * time.Second, 10 * time.Second}
	return GravityOrbProfile{
		Cast: ActionProfile{
			Family: ActionRetainedArea, AbilityName: "SummonGravityOrb",
			AnimationName: "sca_boss_attack_sp",
			HitDelay:      2700 * time.Millisecond, ReleaseDelay: 1500 * time.Millisecond,
			Cooldown: cooldowns[rank-1], Range: 50,
			MovementSpeed: 5, NonCombatMovementSpeed: 3,
			IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true,
		},
		NounName:     "ZelemGravityOrb.Noun",
		BaseOrbCount: 1, AddedOrbPerPlayer: 1,
		MinimumSummonDistance: 6, MaximumSummonDistance: 12,
		MinimumSeparation: 1.5, SummoningArc: math.Pi * 0.5,
		Radius: 8, PlayerPullDistance: pulls[rank-1],
		ProjectilePullDistance: pulls[rank-1] * 100,
		ActivationDelay:        time.Second, InstabilityDelay: 10 * time.Second,
		Lifetime: lifetimes[rank-1], CooldownModifyPeriod: time.Second,
		CooldownModifyAmount: -time.Second, LifecyclePollPeriod: 500 * time.Millisecond,
		StartupEffectName:  "gravity_orb_startup.ServerEventDef",
		StableEffectName:   "gravity_orb_stable.ServerEventDef",
		UnstableEffectName: "gravity_orb_unstable.ServerEventDef",
		FizzleEffectName:   "gravity_orb_fizzle.ServerEventDef",
	}, true
}

func scaldronBossShadowPanicProfile(nounName string) (ActionProfile, bool) {
	rank := scaldronBossRank(nounName)
	if rank == 0 {
		return ActionProfile{}, false
	}
	cooldowns := [...]time.Duration{10 * time.Second, 9 * time.Second, 8 * time.Second}
	ranges := [...]float32{20, 25, 30}
	durations := [...]time.Duration{2 * time.Second, 2500 * time.Millisecond, 3 * time.Second}
	return ActionProfile{
		Family: ActionRetainedArea, AbilityName: "ShadowPanic",
		AnimationName: "sca_boss_attack_su",
		HitDelay:      700 * time.Millisecond, ReleaseDelay: 1633333 * time.Microsecond,
		Cooldown: cooldowns[rank-1], Range: ranges[rank-1], Radius: ranges[rank-1],
		MovementSpeed: 5, NonCombatMovementSpeed: 3,
		ModifierName:         "Modifier_ScaldronBoss_ShadowPanic",
		ModifierDuration:     durations[rank-1],
		IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true,
	}, true
}

func scaldronBossShadowChargeProfile(nounName string) (ActionProfile, bool) {
	if scaldronBossRank(nounName) == 0 {
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionCharge, AbilityName: "ScaldronBoss_ShadowCharge",
		AnimationName: "cast_boomercharge",
		Cooldown:      12 * time.Second, Range: 35,
		MovementSpeed: 5, NonCombatMovementSpeed: 3,
		MovementSpeedBuff: 30,
		MinimumDamage:     20, MaximumDamage: 20,
		DescriptorMask: 1 | 1<<1, DamageType: 4, DamageSource: 0,
		ModifierName: "ShadowBoss_Knockback", ModifierChance: 100,
		ForcedMovementSpeed: 20, ForcedMovementDistance: 6,
		ForcedMovementReactionName: "react_knockback",
		ForcedMovementEffectName:   "charging_trail_effect.ServerEventDef",
		IsDamageProfileKnown:       true, IsFirstAggroDurationKnown: true,
	}, true
}

func scaldronBossDiseaseConeProfile(nounName string) (ActionProfile, bool) {
	rank := scaldronBossRank(nounName)
	if rank == 0 {
		return ActionProfile{}, false
	}
	cooldowns := [...]time.Duration{15 * time.Second, 12500 * time.Millisecond, 10 * time.Second}
	return ActionProfile{
		Family: ActionCone, AbilityName: "VerdanthBossDiseaseCone",
		AnimationName: "sca_boss_attack_lf",
		HitDelay:      1130 * time.Millisecond, ReleaseDelay: 4 * time.Second,
		Cooldown: cooldowns[rank-1], Range: 5,
		MovementSpeed: 5, NonCombatMovementSpeed: 3,
		ModifierName:     "VerdanthBossPrimaryPlague",
		ModifierDuration: 4 * time.Second, ModifierChance: 100,
		ModifierTickDuration:      500 * time.Millisecond,
		ModifierMinimumTickDamage: 8, ModifierMaximumTickDamage: 8,
		ModifierDescriptorMask: 36, ModifierDamageType: 2, ModifierDamageSource: 1,
		ModifierMaximumStack: 1,
		TargetEffectName:     "status_diseased.ServerEventDef",
		Radius:               10, Angle: 45,
		IsModifierDamageProfileKnown: true, IsFirstAggroDurationKnown: true,
	}, true
}
