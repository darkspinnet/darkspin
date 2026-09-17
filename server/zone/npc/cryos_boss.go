package npc

import (
	"strings"
	"time"
)

const cryosBossFirstAggroDuration = 13433334 * time.Microsecond

func CryosBossChainLightningProfile(nounName string) (ActionProfile, bool) {
	rank := cryosBossRank(nounName)
	if rank == 0 {
		return ActionProfile{}, false
	}
	ranges := [...]float32{5, 7.5, 10}
	minimumDamages := [...]float32{8, 12, 16}
	return ActionProfile{
		Family: ActionCone, AbilityName: "ChainLightningBolt",
		AnimationName:               "cry_el_boss_chain_lightning",
		FirstAggroAnimationName:     "cry_el_boss_burrow_out",
		FirstAggroRevealDelay:       2 * time.Second,
		FirstAggroDelay:             cryosBossFirstAggroDuration,
		FirstAggroCinematicDuration: cryosBossFirstAggroDuration,
		FirstAggroCinematicRadius:   100,
		HitDelay:                    2300 * time.Millisecond,
		ReleaseDelay:                4500 * time.Millisecond,
		Cooldown:                    10 * time.Second,
		Range:                       ranges[rank-1],
		MovementSpeed:               5,
		NonCombatMovementSpeed:      3,
		MinimumDamage:               minimumDamages[rank-1],
		MaximumDamage:               30,
		DescriptorMask:              1 << 7,
		DamageType:                  3,
		DamageSource:                1,
		ModifierName:                "CryosBossShock",
		ModifierDuration:            2 * time.Second,
		ModifierChance:              100,
		MaximumTargetCount:          5,
		TrailEffectName:             "cryos_boss_chain_lightning_attack_beam_effect.ServerEventDef",
		SecondaryTrailEffectName:    "cryos_boss_chain_lightning_link_effect.ServerEventDef",
		TargetEffectName:            "cryos_boss_chain_lightning_attack_beam_effect_target.ServerEventDef",
		ImpactEffectName:            "cryos_boss_electric_beam_hit_effect.ServerEventDef",
		Radius:                      10, Angle: 360,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}, true
}

func CryosBossMeleeProfile(nounName string) (ActionProfile, bool) {
	rank := cryosBossRank(nounName)
	if rank == 0 {
		return ActionProfile{}, false
	}
	cooldowns := [...]time.Duration{3 * time.Second, 2500 * time.Millisecond, 2 * time.Second}
	return ActionProfile{
		Family: ActionMelee, AbilityName: "CryosBossMelee",
		AnimationName:               "cry_el_boss_melee_attack",
		FirstAggroAnimationName:     "cry_el_boss_burrow_out",
		FirstAggroRevealDelay:       2 * time.Second,
		FirstAggroDelay:             cryosBossFirstAggroDuration,
		FirstAggroCinematicDuration: cryosBossFirstAggroDuration,
		FirstAggroCinematicRadius:   100,
		HitDelay:                    time.Second,
		ReleaseDelay:                3450 * time.Millisecond,
		Cooldown:                    cooldowns[rank-1],
		Range:                       2.5,
		MovementSpeed:               5,
		NonCombatMovementSpeed:      3,
		MinimumDamage:               14,
		MaximumDamage:               20,
		DescriptorMask:              1 | 1<<1 | 1<<6,
		DamageType:                  3,
		DamageSource:                0,
		ModifierName:                "CryosBoss_Knockback",
		ModifierChance:              100,
		ImpactEffectName:            "plasma_common_electric_hit_medium_effect.ServerEventDef",
		ForcedMovementSpeed:         20,
		ForcedMovementDistance:      15,
		ForcedMovementReactionName:  "react_knockback",
		Radius:                      3.5, Angle: 360,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}, true
}

func cryosBossRank(nounName string) int {
	switch strings.ToLower(nounName) {
	case "cryosboss.noun":
		return 1
	case "cryosboss_2.noun":
		return 2
	case "cryosboss_3.noun":
		return 3
	default:
		return 0
	}
}
