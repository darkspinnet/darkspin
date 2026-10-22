package gameplay

import (
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
)

// projectOverdrivePassiveRank restores the third-slot operands consumed by
// implicit common helper IsInOverdrive (chunk827). Ordinary defaults are
// recovered with the binding; these additions operate on a temporary copy.
func projectOverdrivePassiveRank(creature game.GameplayCreature) game.GameplayCreature {
	switch creature.PassiveAbility {
	case util.HashID("CrushingDread"):
		// Chunk944: radius {12,12,16,16}, both reductions {.15,.15,.30,.30}.
		creature.PassiveAuraRadius = 16
		creature.PhysicalDamageReduction += 0.15
		creature.EnergyDamageReduction += 0.15
	case util.HashID("RampantGrowth"):
		// Chunk366's shared aura gives both DoT and HoT the same operand.
		creature.DamageProfile.DamageOverTimeIncrease += 0.25
		creature.HealingProfile.HealingOverTimeIncrease += 0.25
	case util.HashID("GravityTempestPassive"):
		// Chunk237: projectile speed {.25,.25,1,1}, damage {.10,.10,.25,.25}.
		creature.TimingProfile.ProjectileSpeedIncrease += 0.75
		creature.DamageProfile.ProjectileDamage += 0.15
	case util.HashID("LightspeedTempestPassive"):
		// Chunk671 evaluates these maxima against current health each tick.
		creature.PassiveHealthAttackMax = 0.30
		creature.PassiveHealthMovementMax = 0.35
	case util.HashID("VoodooTempestPassive"):
		creature.PassiveAuraRadius = 12
	case util.HashID("TimeRavagerPassiveModifier"):
		creature.PassiveAuraRadius = 16
		// Companion haste chunk652 retains .10 attack speed, doubles movement.
		creature.PassiveMovementIncrease += 0.06
	case util.HashID("SoulRavagerPassive"):
		creature.PassiveKillDamageIncrease += 0.03
	case util.HashID("BinarySentinelPassive"):
		creature.DamageProfile.BasicDefenseBoost += 0.005
	}
	return creature
}
