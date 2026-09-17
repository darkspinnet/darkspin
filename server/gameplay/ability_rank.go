package gameplay

import (
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
)

// projectHeroAbilityRank applies only rank operands recovered from packaged
// ability Lua. The content catalog remains the rank-one source projection.
func projectHeroAbilityRank(
	definition sim.AbilityDefinition, rank int32,
) sim.AbilityDefinition {
	if rank <= 1 {
		return definition
	}
	if rank%2 == 0 {
		definition = projectEvenHeroAbilityRank(definition)
	}
	switch definition.Name {
	case "FireRavagerSupport":
		if rank == 5 || rank == 6 {
			definition.StatusDuration = 4 * time.Second
		}
		if rank >= 7 {
			definition.ShotCount = 3
			definition.HitDelays = []time.Duration{
				200 * time.Millisecond, 400 * time.Millisecond,
				600 * time.Millisecond,
			}
			definition.RootModifierID = 0
			definition.StatusDuration = 0
			definition.StatusEnergyIncrease = 0
		}
	case "Terrify":
		if rank == 5 || rank == 6 {
			definition.StatusKind = sim.AbilityStatusKindStun
			definition.RootModifierID = util.HashID("TerrifyFreezeDebuff")
		}
		if rank >= 7 {
			definition.Radius = 3
			definition.SecondaryModifierID = util.HashID("TerrifyDebuffNoDot")
		}
	case "TCShieldedSentinelSupport":
		if rank >= 7 {
			definition.SecondaryMinimumDamage = definition.MinimumDamage
			definition.SecondaryMaximumDamage = definition.MaximumDamage
			definition.SecondaryDamageCoefficient = definition.DamageCoefficient
			definition.ManaCost = 24
			definition.ManaCoefficient = 0.12
		}
		if rank == 5 || rank == 6 {
			definition.StatusKind = sim.AbilityStatusKindSilence
			definition.RootModifierID = util.HashID(
				"TCShieldedSentinel_Support_SilenceUpgradeModifier",
			)
			definition.StatusDuration = 3 * time.Second
			if rank == 6 {
				definition.StatusDuration = time.Second
			}
		}
	case "RootingPlague":
		if rank%2 == 0 {
			definition.StatusDuration = 6 * time.Second
		} else {
			definition.StatusDuration = 8 * time.Second
		}
	case "BinarySentinelSupport":
		if rank == 5 || rank == 6 {
			definition.MinimumDamagePercent = 0.625
			definition.IsFixedDamageScale = true
		}
		if rank >= 7 {
			definition.ManaCost = 24
			definition.ManaCoefficient = 0.12
		}
		if rank == 8 {
			definition.LargeTargetDamageMultiplier = 1.5
		}
	}
	return definition
}

func projectEvenHeroAbilityRank(
	definition sim.AbilityDefinition,
) sim.AbilityDefinition {
	switch definition.Name {
	case "PlasmaRandom":
		definition.StatusDuration = 2 * time.Second
	case "ClaymoreTrap":
		definition.StatusDuration = 3 * time.Second
	case "PipeBomb":
		definition.StatusDuration = time.Second
	case "NecroRandom":
		definition.MinimumDamagePerTick = 8
		definition.MaximumDamagePerTick = 8
		definition.MinimumHealingPerTick = 8
		definition.MaximumHealingPerTick = 8
		definition.StatusDamageReduction = 0.25
		definition.Cooldown = 8 * time.Second
	case "SoulRavagerSupport":
		definition.MinimumDamage = 6
		definition.MaximumDamage = 12
		definition.Cooldown = 8 * time.Second
	case "PlasmaRandom_WebbedLightning":
		definition.StatusDuration = time.Second
	case "EntanglingRush":
		definition.MinimumDamage = 16
		definition.MaximumDamage = 24
		definition.StatusDuration = 4 * time.Second
	case "PhantomCharge":
		definition.StatusDuration = time.Second
	case "SpacetimeRandom":
		definition.Duration = 6 * time.Second
	case "TimeRavagerSupport":
		definition.StatusDuration = time.Second
	case "TechRandom2":
		definition.Duration = 3 * time.Second
	case "TechRandom3":
		definition.MinimumDamage = 25
		definition.MaximumDamage = 35
		definition.StatusDuration = 3 * time.Second
	case "Terrify":
		definition.Cooldown = 12 * time.Second
		definition.HitDelays = []time.Duration{0, time.Second, 2 * time.Second}
		definition.StatusDuration = 2100 * time.Millisecond
		definition.MinimumDamagePerTick = 7
		definition.MaximumDamagePerTick = 7
	case "Psistorm":
		definition.Duration = 3 * time.Second
		definition.NumberOfTicks = 4
	case "VoodooTempestSupport":
		definition.Duration = 6 * time.Second
		definition.StatusDamageBuff = -0.25
		definition.StatusPhysicalIncrease = 0.25
		definition.StatusEnergyIncrease = 0.25
	case "TCShieldedSentinelSupport":
		definition.MinimumDamage = 16
		definition.MaximumDamage = 24
	case "BioRandom2":
		definition.TickDuration = 3 * time.Second
		definition.StatusDuration = 3 * time.Second
	case "TCShieldedSentinelActive":
		definition.StatusDuration = 3 * time.Second
	case "QuantumBlink":
		definition.Cooldown = 18 * time.Second
		definition.MinimumDamage = 2
		definition.MaximumDamage = 8
	case "SummonBeast":
		definition.Cooldown = 16 * time.Second
	}
	return definition
}
