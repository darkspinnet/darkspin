package npc

import (
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

const (
	nashiraFirstAggroStartDelay = 2 * time.Second
	nashiraFirstAggroDuration   = 9600 * time.Millisecond
	nashiraMovementSpeed        = 7
	nashiraNonCombatSpeed       = 5.5
)

// NashiraCloneProfile keeps combat behavior without replaying the boss entrance.
func NashiraCloneProfile(profile ActionProfile) ActionProfile {
	profile.FirstAggroAnimationName = ""
	profile.FirstAggroEffectName = ""
	profile.FirstAggroEffectDelay = 0
	profile.FirstAggroRevealDelay = 0
	profile.FirstAggroDelay = 0
	profile.FirstAggroCinematicDuration = 0
	profile.FirstAggroCinematicRadius = 0
	profile.IsFirstAggroDurationKnown = true
	profile.PassiveEffectName = "shadow_boss_duplicate_shader_effect.ServerEventDef"
	return profile
}

func NashiraShadowTossProfile(nounName string) (ActionProfile, bool) {
	rank := shadowBossRank(nounName)
	if rank == 0 {
		return ActionProfile{}, false
	}
	cooldowns := [...]time.Duration{6 * time.Second, 5 * time.Second, 4 * time.Second}
	ranges := [...]float32{16, 18, 20}
	minimumDamages := [...]float32{18, 24, 30}
	maximumDamages := [...]float32{24, 30, 36}
	projectileSpeeds := [...]float32{4, 6, 8}
	index := rank - 1
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "ShadowToss",
		AnimationName:                 "cast_shadowtoss_far",
		NearAnimationName:             "cast_shadowtoss_near",
		FarAnimationName:              "cast_shadowtoss_far",
		FirstAggroAnimationName:       "shadowboss_onaggro",
		FirstAggroEffectName:          "black_hole_effect",
		FirstAggroEffectDelay:         nashiraFirstAggroStartDelay,
		FirstAggroRevealDelay:         nashiraFirstAggroStartDelay,
		FirstAggroDelay:               nashiraFirstAggroDuration,
		FirstAggroCinematicDuration:   nashiraFirstAggroDuration,
		FirstAggroCinematicRadius:     100,
		HitDelay:                      930 * time.Millisecond,
		ReleaseDelay:                  2 * time.Second,
		Cooldown:                      cooldowns[index],
		Range:                         ranges[index],
		MovementSpeed:                 nashiraMovementSpeed,
		NonCombatMovementSpeed:        nashiraNonCombatSpeed,
		MinimumDamage:                 minimumDamages[index],
		MaximumDamage:                 maximumDamages[index],
		DescriptorMask:                8320,
		DamageType:                    4,
		DamageSource:                  1,
		ProjectileNoun:                "Ability_Epic_Fireball.Noun",
		TrailEffectName:               "shadow_lob01.ServerEventDef",
		ImpactEffectName:              "shadowtoss_impact.ServerEventDef",
		ProjectileSpeed:               projectileSpeeds[index],
		ProjectileHeight:              12,
		ProjectileCloseRange:          8,
		ProjectileCloseHeight:         12,
		ProjectileCloseSpeed:          projectileSpeeds[index],
		Radius:                        1,
		ProjectileOffset:              game.Vec3{X: 3, Y: 5, Z: 3},
		ProjectileCloseOffset:         game.Vec3{X: -5, Y: 3, Z: 3},
		IsDamageProfileKnown:          true,
		IsFirstAggroDurationKnown:     true,
		IsProjectileCloseOffsetXFound: true,
	}, true
}

func NashiraSwipeProfile(nounName string) (ActionProfile, bool) {
	rank := shadowBossRank(nounName)
	if rank == 0 {
		return ActionProfile{}, false
	}
	cooldowns := [...]time.Duration{5 * time.Second, 4 * time.Second, 3 * time.Second}
	index := rank - 1
	return ActionProfile{
		Family: ActionMelee, AbilityName: "ShadowBossSwipe",
		AnimationName:               "shadow_boss_swipe_a",
		FirstAggroAnimationName:     "shadowboss_onaggro",
		FirstAggroEffectName:        "black_hole_effect",
		FirstAggroEffectDelay:       nashiraFirstAggroStartDelay,
		FirstAggroRevealDelay:       nashiraFirstAggroStartDelay,
		FirstAggroDelay:             nashiraFirstAggroDuration,
		FirstAggroCinematicDuration: nashiraFirstAggroDuration,
		FirstAggroCinematicRadius:   100,
		HitDelay:                    1167 * time.Millisecond,
		ReleaseDelay:                3100 * time.Millisecond,
		Cooldown:                    cooldowns[index],
		Range:                       6,
		MovementSpeed:               nashiraMovementSpeed,
		NonCombatMovementSpeed:      nashiraNonCombatSpeed,
		MinimumDamage:               12,
		MaximumDamage:               24,
		DescriptorMask:              67,
		DamageType:                  4,
		DamageSource:                0,
		ImpactEffectName:            "necro_common_hit_large.ServerEventDef",
		IsDamageProfileKnown:        true,
		IsFirstAggroDurationKnown:   true,
	}, true
}

func NashiraSwipeAnimation(actionIndex uint64) string {
	animations := [...]string{
		"shadow_boss_swipe_a",
		"shadow_boss_swipe_b",
		"shadow_boss_swipe_c",
		"shadow_boss_swipe_d",
	}
	return animations[actionIndex%uint64(len(animations))]
}

func NashiraPanicProfile(nounName string) (ActionProfile, bool) {
	if shadowBossRank(nounName) == 0 {
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionRetainedArea, AbilityName: "ShadowPanic",
		AnimationName:             "cast_shadow_panic",
		HitDelay:                  400 * time.Millisecond,
		ReleaseDelay:              2533 * time.Millisecond,
		Cooldown:                  15 * time.Second,
		Range:                     12,
		Radius:                    12,
		ModifierName:              "FearNova",
		ModifierDuration:          3 * time.Second,
		MovementSpeed:             nashiraMovementSpeed,
		NonCombatMovementSpeed:    nashiraNonCombatSpeed,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}, true
}

func NashiraFiendNoun(nounName string) (string, bool) {
	switch shadowBossRank(nounName) {
	case 1:
		return "ShadowBossMinion.Noun", true
	case 2:
		return "ShadowBossMinion_2.Noun", true
	case 3:
		return "ShadowBossMinion_3.Noun", true
	default:
		return "", false
	}
}

func NashiraFiendProfile(nounName string) (ActionProfile, bool) {
	var rank int
	switch strings.ToLower(nounName) {
	case "shadowbossminion.noun":
		rank = 1
	case "shadowbossminion_2.noun":
		rank = 2
	case "shadowbossminion_3.noun":
		rank = 3
	default:
		return ActionProfile{}, false
	}
	profile := shooterPoisonSpitProfile(
		4*time.Second, 18, 5+float32(rank), 5+float32(rank), 4, true,
	)
	profile.AbilityName = "ShadowBossMinionBolt"
	profile.AnimationName = "nct_minn_su_x3_attack1"
	profile.FirstAggroAnimationName = "gen_aggro_su_voidzone"
	profile.FirstAggroEffectName = "shadow_lob_impact_spawn_creature"
	profile.MinimumDamage = 3 + float32(rank)
	profile.MaximumDamage = 6 + float32(rank*2)
	return profile, true
}

func IsTossActionProfile(profile ActionProfile) bool {
	switch profile.AbilityName {
	case "NoctFlyerLob", "SleepMushroom", "CitadelGrenadeRoll", "HurlMagma",
		"NomadShielderGrenade", "ShadowToss":
		return true
	default:
		return false
	}
}

func IsNashiraNoun(nounName string) bool {
	return shadowBossRank(nounName) != 0
}

func shadowBossRank(nounName string) int {
	switch strings.ToLower(nounName) {
	case "shadowboss.noun":
		return 1
	case "shadowboss_2.noun":
		return 2
	case "shadowboss_3.noun":
		return 3
	default:
		return 0
	}
}
