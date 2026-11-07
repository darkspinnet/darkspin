package npc

import (
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

// IsArcturusTurret identifies the boss-owned source whose passive owns its
// laser sweep. It must never enter the ordinary agent attack scheduler.
func IsArcturusTurret(plan SpawnPlan) bool {
	if plan.OwnerObjectID == 0 {
		return false
	}
	return strings.EqualFold(plan.NounName, "CitadelBossTurret_v1.Noun") ||
		strings.EqualFold(plan.NounName, "CitadelBossTurret_v2.Noun")
}

// SetArcturusTurretExposed mirrors build-103 TurretCheck/Launch: full physics
// is removed while the boss is grounded and restored during its airborne phase.
// Full removal also removes navigation blocking; packet publication must use
// this same state rather than leaving an invisible server-side obstacle.
func (e *Session) SetArcturusTurretExposed(objectID uint32, isExposed bool, protectedUntil time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || actor.IsDefeated || !IsArcturusTurret(actor.Plan) || protectedUntil.IsZero() {
		return fmt.Errorf("turretPhaseUnavailable: %d", objectID)
	}
	if isExposed {
		if actor.status.intangibleExpiresAt == protectedUntil {
			actor.status.intangibleExpiresAt = time.Time{}
		}
	} else if protectedUntil.After(actor.status.intangibleExpiresAt) {
		actor.status.intangibleExpiresAt = protectedUntil
	}
	actor.IsNavigationCollisionEnabled = isExposed
	e.npcs[objectID] = actor
	return nil
}

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
