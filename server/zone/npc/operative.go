package npc

import (
	"strings"
	"time"
)

// OperativeProfile uses the shipped NomadCyberOne_Cage and
// NomadSpacetimeAgent_Cage Lua properties. Damage is one channel tick; the
// gameplay runtime keeps the cage bound to its living, uninterrupted caster.
func OperativeProfile(nounName string) (ActionProfile, bool) {
	family := strings.TrimSuffix(strings.ToLower(nounName), ".noun")
	family = strings.TrimSuffix(strings.TrimSuffix(family, "_2"), "_3")
	profile := ActionProfile{
		Family: ActionMelee, Range: 10, MovementSpeed: 5, NonCombatMovementSpeed: 5,
		MinimumDamage: 15, MaximumDamage: 15, DamageSource: 1,
		HitDelay: 1900 * time.Millisecond, Cooldown: 2 * time.Second,
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
	case "nomadspacetimeagent":
		profile.AbilityName = "NomadSpacetimeAgent_Cage"
		profile.AnimationName = "nomad_lieu_sp_2_attack1"
		profile.EndAnimationName = "nomad_lieu_sp_2_attack1_end"
		profile.ModifierName = "NomadSpacetimeAgent_CageModifier"
		profile.Range = 12
		profile.HitDelay = 800 * time.Millisecond
		profile.DamageType = 1
	default:
		return ActionProfile{}, false
	}
	profile.ReleaseDelay = profile.HitDelay
	profile.Cooldown += profile.HitDelay
	return profile, true
}

func IsOperativeCage(modifierName string) bool {
	return modifierName == "NomadCyberOne_CageModifier" ||
		modifierName == "NomadSpacetimeAgent_CageModifier"
}
