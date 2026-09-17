package npc

import (
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

func ArcturusRank(nounName string) int {
	switch strings.ToLower(nounName) {
	case "citadelboss.noun":
		return 1
	case "citadelboss_2.noun":
		return 2
	case "citadelboss_3.noun":
		return 3
	default:
		return 0
	}
}

// ArcturusSawBladeProfile follows packaged Lua chunk 415, including its
// non-homing, piercing projectile and rank-three target leading.
func ArcturusSawBladeProfile(nounName string) (ActionProfile, bool) {
	rank := ArcturusRank(nounName)
	if rank == 0 {
		return ActionProfile{}, false
	}
	profile, isFound := citadelBossTwinLaserProfile(nounName)
	if !isFound {
		return ActionProfile{}, false
	}
	cooldowns := [...]time.Duration{4 * time.Second, 3500 * time.Millisecond, 3 * time.Second}
	speeds := [...]float32{20, 30, 40}
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "SawBladeShot",
		AnimationName: "ctd_boss_tc_attack3",
		HitDelay:      333333343 * time.Nanosecond, ReleaseDelay: 966666996 * time.Nanosecond,
		Cooldown: cooldowns[rank-1], Range: 40,
		MovementSpeed: profile.MovementSpeed, NonCombatMovementSpeed: profile.NonCombatMovementSpeed,
		MinimumDamage: 8, MaximumDamage: 12, DescriptorMask: 8256,
		DamageSource: 0, DamageType: 0, IsDamageProfileKnown: true,
		ProjectileNoun: "Ability_Fireball.Noun", ProjectileSpeed: speeds[rank-1],
		ProjectileDistance: 50, ProjectileOffset: game.Vec3{Z: -2},
		// timetohit is a three-entry shot sequence, not a single delay.
		ProjectileShotCount: 3, ProjectileShotInterval: 333333343 * time.Nanosecond,
		IsProjectilePiercing: true, IsProjectileTrackingBetweenShots: rank >= 2,
		IsProjectileLeadingTarget: rank == 3,
		TrailEffectName:           "citadel_boss_saw_blade.ServerEventDef",
		ImpactEffectName:          "citadel_boss_saw_blade_hit.ServerEventDef",
		SourceEffectName:          "citadel_boss_saw_blade_muzzle.ServerEventDef",
		MissEffectName:            "ineffective_common_small.ServerEventDef",
		IsFirstAggroDurationKnown: true,
	}, true
}
