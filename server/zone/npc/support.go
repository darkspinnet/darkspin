package npc

import (
	"strings"
	"time"
)

// SupportSpell describes one authored instant cast, its condition, and modifier.
// Build-103 chunks 483/1025/752/433/21/587 and 823/296/198/926/586/151.
type SupportSpell struct {
	Profile              ActionProfile
	ConditionRadius      float32
	IsAlly               bool
	IsSelfAllowed        bool
	IsIndefinite         bool
	EnergyDamageIncrease float32
}

func SupportSpells(plan SpawnPlan) []SupportSpell {
	name := strings.ToLower(plan.NounName)
	isScope := name == "nomadscope.noun" || name == "nomadscope_2.noun" || name == "nomadscope_3.noun" ||
		name == "nomadscope_captain.noun" || name == "nomadscope_captain_2.noun" || name == "nomadscope_captain_3.noun"
	isCitadel := name == "citadelspecialone.noun" || name == "citadelspecialone_2.noun" || name == "citadelspecialone_3.noun"
	if !isScope && !isCitadel {
		return nil
	}
	index := EffectiveNPCRank(plan) - 1
	profile := ActionProfile{Family: ActionMelee, DescriptorMask: 1 << 7, DamageType: 3, DamageSource: 1,
		IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true}
	if isScope {
		profile.MovementSpeed, profile.NonCombatMovementSpeed = 8.5, 8.5
		profile.Range = [...]float32{15, 18, 21}[index]
		profile.FirstAggroAnimationName = "first_aggro_roar"
		profile.FirstAggroDelay = 1750 * time.Millisecond
		burn := profile
		burn.AbilityName, burn.ModifierName = "CastScopeBurn", "ScopeBurn"
		burn.AnimationName = "nomad_lieu_el_2_burningDOT_cast"
		burn.HitDelay, burn.ReleaseDelay, burn.Cooldown = 366666675*time.Nanosecond, 1933333039*time.Nanosecond, 4*time.Second
		burn.ModifierDuration = [...]time.Duration{6 * time.Second, 8 * time.Second, 10 * time.Second}[index]
		burn.ModifierTickDuration, burn.ModifierTickDamage, burn.ModifierMaximumStack = 2*time.Second, 4, 1
		burn.ModifierDescriptorMask, burn.ModifierDamageType, burn.ModifierDamageSource = 1<<2|1<<5|1<<7, 3, 1
		burn.IsModifierDamageProfileKnown = true
		burn.ImpactEffectName = "nomad_lieu_el_2_scope_fire_burningDOT_hit_effect.ServerEventDef"
		debuff := profile
		debuff.AbilityName, debuff.ModifierName = "CastScopeDebuff", "ScopeDebuff"
		debuff.AnimationName = "nomad_lieu_el_2_energydebuff_cast"
		debuff.HitDelay, debuff.ReleaseDelay, debuff.Cooldown = 600000024*time.Nanosecond, 2166666985*time.Nanosecond, 6*time.Second
		debuff.ModifierDuration = [...]time.Duration{8 * time.Second, 10 * time.Second, 12 * time.Second}[index]
		debuff.ImpactEffectName = "nomad_lieu_el_2_scope_debuff_hit_effect.ServerEventDef"
		thorns := profile
		thorns.AbilityName, thorns.ModifierName = "CastScopeThorns", "ScopeThorns"
		thorns.Range = [...]float32{12, 15, 18}[index]
		thorns.AnimationName = "nomad_lieu_el_2_firethorns_cast"
		thorns.HitDelay, thorns.ReleaseDelay, thorns.Cooldown = 533333361*time.Nanosecond, 1766667008*time.Nanosecond, 2*time.Second
		thorns.ModifierDuration = [...]time.Duration{10 * time.Second, 12 * time.Second, 15 * time.Second}[index]
		thorns.ImpactEffectName = "nomad_lieu_el_2_scope_fire_thorns_hit_effect.ServerEventDef"
		return []SupportSpell{
			{Profile: burn, ConditionRadius: 25},
			{Profile: debuff, ConditionRadius: 25, EnergyDamageIncrease: [...]float32{.5, .75, 1}[index]},
			{Profile: thorns, ConditionRadius: 20, IsAlly: true},
		}
	}
	profile.Range, profile.MovementSpeed, profile.NonCombatMovementSpeed = 10, plan.NPCProfile.BaseCombatSpeed, plan.NPCProfile.IdleMovementSpeed
	// The old instant-cast template has no per-ability cooldown override here.
	// Its hit precedes ReleaseAgent even though timetorelease is only .3s.
	profile.HitDelay, profile.ReleaseDelay = 500*time.Millisecond, 500*time.Millisecond
	burn := profile
	burn.AbilityName, burn.ModifierName = "CitadelSpecialOne_BurningDOT", "OnFire"
	burn.AnimationName, burn.ImpactEffectName = "cast_frostbite", "instantnova_hit.ServerEventDef"
	burn.ModifierDuration = [...]time.Duration{6 * time.Second, 8 * time.Second, 10 * time.Second}[index]
	burn.ModifierTickDuration, burn.ModifierMinimumTickDamage, burn.ModifierMaximumTickDamage = 2*time.Second, 6, 12
	burn.ModifierMaximumStack, burn.ModifierTickDamageCoefficient = 1, .05
	burn.ModifierDescriptorMask, burn.ModifierDamageType, burn.ModifierDamageSource = 1<<2|1<<5, 3, 1
	burn.IsModifierDamageProfileKnown = true
	debuff := profile
	debuff.AbilityName, debuff.ModifierName = "CitadelSpecialOne_EnergyVulnerable", "CitadelSpecialOne_EnergyDefenseDebuff"
	debuff.AnimationName, debuff.ModifierDuration = "healer_root", 3*time.Second
	thorns := profile
	thorns.AbilityName, thorns.ModifierName = "CitadelSpecialOne_FireThorns", "Modifier_FireThorns"
	thorns.AnimationName = "cast_frostbite"
	return []SupportSpell{
		{Profile: burn, ConditionRadius: 20},
		{Profile: debuff, ConditionRadius: 20, EnergyDamageIncrease: .1},
		{Profile: thorns, ConditionRadius: 20, IsAlly: true, IsSelfAllowed: true, IsIndefinite: true},
	}
}
