package npc

import (
	"strings"
	"time"
)

// IsAgentHealingChild identifies the child whose passive owner schedules heals.
// Its target link is ownership context, never admission to a combat pursuit.
func IsAgentHealingChild(plan SpawnPlan) bool {
	return plan.IsActionKnown && plan.ActionProfile.AbilityName == "NomadCyberOne_Droid_Heal"
}

func NomadPetsMeleeProfile() ActionProfile {
	return ActionProfile{Family: ActionMelee, AbilityName: "NomadPetsMelee", AnimationName: "nomad_lieu_el_3_melee_attack", Cooldown: 1500 * time.Millisecond, Range: 1.5, Radius: 2.5, HitDelay: 242424249 * time.Nanosecond, ReleaseDelay: 1500 * time.Millisecond, MinimumDamage: 5, MaximumDamage: 8, DescriptorMask: 67, DamageType: 3, DamageSource: 0, MovementSpeed: 5, IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true, ImpactEffectName: "generic_impacts_charge_impact_effect.ServerEventDef"}
}

func AgentChildProfile(nounName string, rank int32) ActionProfile {
	family := strings.TrimSuffix(strings.ToLower(nounName), ".noun")
	family = strings.TrimSuffix(strings.TrimSuffix(family, "_2"), "_3")
	profile := ActionProfile{Family: ActionUnknown, AbilityName: "NomadCyberOne_Droid_Heal", MinimumHealing: 3, MaximumHealing: 3, TickDuration: time.Second, NumberOfTicks: 301, IsFirstAggroDurationKnown: true}
	if family == "nomadcyberone_droid" {
		return profile
	}
	rankIndex := max(0, min(int(rank)-1, 2))
	if family == "nomadfirepet" {
		return ActionProfile{Family: ActionMelee, AbilityName: "FireStream", AnimationName: "nomad_lieu_el_3_firepet_attack_start", LoopAnimationName: "nomad_lieu_el_3_firepet_attack_loop", EndAnimationName: "nomad_lieu_el_3_firepet_attack_end", Range: 3, Radius: 3, Angle: 90, HitDelay: 466666996 * time.Nanosecond, ReleaseDelay: 466666996 * time.Nanosecond, TickDuration: time.Second, Cooldown: time.Second, MinimumDamage: [...]float32{5, 8, 10}[rankIndex], MaximumDamage: [...]float32{10, 16, 20}[rankIndex], DamageType: 3, DamageSource: 1, DescriptorMask: 1024, MovementSpeed: 5, IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true}
	}
	return ActionProfile{Family: ActionMelee, AbilityName: "ChannelLightningCage", AnimationName: "nomad_lieu_el_3_lightningpet_attack_start", LoopAnimationName: "nomad_lieu_el_3_lightningpet_attack_loop", EndAnimationName: "nomad_lieu_el_3_lightningpet_attack_end", Range: [...]float32{5, 6, 7}[rankIndex], HitDelay: 466666996 * time.Nanosecond, ReleaseDelay: 466666996 * time.Nanosecond, Cooldown: 4 * time.Second, EndAnimationDelay: 266667008 * time.Nanosecond, ModifierName: "LightningCage", ModifierDuration: 0, MovementSpeed: 5, IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true}
}
