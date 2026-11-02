package npc

import (
	"strings"
	"time"
)

// NomadSpecialOneCleaveProfile is the independent build-103 melee action,
// Abilities/0x0823B75D.lua, rather than the charge landing's contact attack.
func NomadSpecialOneCleaveProfile(nounName string) ActionProfile {
	rankIndex := 0
	family := strings.TrimSuffix(strings.ToLower(nounName), ".noun")
	if strings.HasSuffix(family, "_2") {
		rankIndex = 1
	}
	if strings.HasSuffix(family, "_3") {
		rankIndex = 2
	}
	return ActionProfile{
		Family: ActionMelee, AbilityName: "NomadSpecialOneCleave",
		AnimationName: "nomad_lieu_lf_1_attack1",
		Range:         2, Radius: 2.75, Angle: 180, MaximumTargetCount: 1,
		HitDelay: 629999995 * time.Nanosecond, ReleaseDelay: 2 * time.Second,
		Cooldown:      [...]time.Duration{3 * time.Second, 2500 * time.Millisecond, 2 * time.Second}[rankIndex],
		MinimumDamage: 7, MaximumDamage: 12, DescriptorMask: 1 | 1<<1 | 1<<6,
		DamageType: 2, DamageSource: 0, MovementSpeed: 5, NonCombatMovementSpeed: 5,
		ImpactEffectName:     "nomad_lieu_lf_1_cleave_hit.ServerEventDef",
		IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true,
	}
}
