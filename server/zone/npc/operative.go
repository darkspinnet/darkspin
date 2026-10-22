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
		modifierName == "NomadSpacetimeAgent_CageModifier"
}
