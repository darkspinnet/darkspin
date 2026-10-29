package npc

import (
	"strings"
	"time"
)

// OperativeProfile uses the shipped NomadCyberOne_Cage and
// NomadSpacetimeAgent_Cage Lua properties. Damage describes the initial hit
// and each authored pulse; the gameplay owner retains their distinct loops.
func OperativeProfile(nounName string) (ActionProfile, bool) {
	family := strings.TrimSuffix(strings.ToLower(nounName), ".noun")
	rankIndex := 0
	if strings.HasSuffix(family, "_2") {
		rankIndex = 1
		family = strings.TrimSuffix(family, "_2")
	} else if strings.HasSuffix(family, "_3") {
		rankIndex = 2
		family = strings.TrimSuffix(family, "_3")
	}
	profile := ActionProfile{
		Family: ActionMelee, Range: 10, MovementSpeed: 5, NonCombatMovementSpeed: 5,
		MinimumDamage: 15, MaximumDamage: 15, DamageSource: 1,
		HitDelay:             1899999976 * time.Nanosecond,
		TickDuration:         [...]time.Duration{2 * time.Second, 1500 * time.Millisecond, time.Second}[rankIndex],
		ModifierDuration:     1000 * time.Second,
		IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true,
	}
	switch family {
	case "nomadcyberone":
		profile.AbilityName = "NomadCyberOne_Cage"
		profile.AnimationName = "nomad_lieu_tc_4_attack1"
		profile.EndAnimationName = "nomad_lieu_tc_4_attack1_end"
		profile.ModifierName = "NomadCyberOne_CageModifier"
		profile.ModifierID = 0x04d7ca69
		profile.Range = [...]float32{10, 12, 14}[rankIndex]
		profile.Cooldown = [...]time.Duration{6 * time.Second, 5 * time.Second, 4 * time.Second}[rankIndex]
	case "nomadspacetimeagent":
		profile.AbilityName = "NomadSpacetimeAgent_Cage"
		profile.AnimationName = "nomad_lieu_sp_2_attack1"
		profile.EndAnimationName = "nomad_lieu_sp_2_attack1_end"
		profile.ModifierName = "NomadSpacetimeAgent_CageModifier"
		profile.Range = [...]float32{12, 14, 16}[rankIndex]
		profile.Cooldown = [...]time.Duration{5 * time.Second, 4 * time.Second, 3 * time.Second}[rankIndex]
		profile.HitDelay = 800000011 * time.Nanosecond
		profile.DamageType = 1
	default:
		return ActionProfile{}, false
	}
	profile.ReleaseDelay = profile.HitDelay
	return profile, true
}

func IsOperativeCage(modifierName string) bool {
	return modifierName == "NomadCyberOne_CageModifier" ||
		modifierName == "NomadSpacetimeAgent_CageModifier" || modifierName == "NocturnaSpecialHaunt_LockdownModifier" || modifierName == "LightningCage" || modifierName == "NomadSpecialFour_CageModifier"
}

// SpacetimeBlinkProfile follows build-103 chunk445. Its low-health admission
// belongs to the phase selector, independent of the long blink cooldown.
func SpacetimeBlinkProfile(nounName string) (ActionProfile, bool) {
	family := strings.TrimSuffix(strings.ToLower(nounName), ".noun")
	rankIndex := 0
	if strings.HasSuffix(family, "_2") {
		rankIndex = 1
		family = strings.TrimSuffix(family, "_2")
	}
	if strings.HasSuffix(family, "_3") {
		rankIndex = 2
		family = strings.TrimSuffix(family, "_3")
	}
	if family != "nomadspacetimeagent" {
		return ActionProfile{}, false
	}
	return ActionProfile{Family: ActionMelee, AbilityName: "NomadSpacetimeAgent_Blink", AnimationName: "nomad_lieu_sp_2_blink_out", HitDelay: 1299999952 * time.Nanosecond, ReleaseDelay: 1299999952 * time.Nanosecond, Cooldown: 300 * time.Second, Range: [...]float32{15, 20, 25}[rankIndex], TeleportMinimumDistance: 10, TeleportNormalDistance: 13, TeleportMaximumDistance: 20, TeleportAnimationName: "nomad_lieu_sp_2_blink_in", TeleportAnimationDelay: 2329999983 * time.Nanosecond, IsFirstAggroDurationKnown: true}, true
}

// HauntLockdownProfile uses build-103 chunks365/351/383. The phase's
// MostIsolatedEnemy and Closest are alternate admission conditions for one cast.
func HauntLockdownProfile(nounName string) (ActionProfile, bool) {
	family := strings.TrimSuffix(strings.ToLower(nounName), ".noun")
	rankIndex := 0
	if strings.HasSuffix(family, "_2") {
		rankIndex = 1
		family = strings.TrimSuffix(family, "_2")
	}
	if strings.HasSuffix(family, "_3") {
		rankIndex = 2
		family = strings.TrimSuffix(family, "_3")
	}
	if family != "nocturnaspecialhaunt" {
		return ActionProfile{}, false
	}
	return ActionProfile{Family: ActionMelee, AbilityName: "NocturnaSpecialHauntLockdown", AnimationName: "nomad_lieu_su_1_attack1_start", LoopAnimationName: "nomad_lieu_su_1_attack1_loop", EndAnimationName: "nomad_lieu_su_1_attack1_end", Range: 0.5, MovementSpeed: 5, HitDelay: 300000011 * time.Nanosecond, ReleaseDelay: 2333333015 * time.Nanosecond, Cooldown: [...]time.Duration{12 * time.Second, 8 * time.Second, 4 * time.Second}[rankIndex], TickDuration: 1133332967 * time.Nanosecond, EndAnimationDelay: 566667020 * time.Nanosecond, MinimumDamage: 6, MaximumDamage: 10, DescriptorMask: 128, DamageType: 4, DamageSource: 1, ModifierName: "NocturnaSpecialHaunt_LockdownModifier", ModifierDuration: 0, MaximumChannelDistance: 8, Radius: 10, HealingCoefficient: 0, IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true}, true
}
