package game

import (
	"errors"
	"math"
)

// AbilityDamage describes the authored inputs consumed by build-103's
// attacker-side damage projection before critical and target-side reduction.
type AbilityDamage struct {
	Minimum             float32
	Maximum             float32
	Coefficient         float32
	Descriptor          uint32
	DamageType          uint8
	DamageSource        uint8
	IsDescriptorFound   bool
	IsDamageTypeFound   bool
	IsDamageSourceFound bool
}

// DamageProfile is the authenticated attacker snapshot used by the recovered
// build-103 descriptor multiplier and direct-damage stages.
type DamageProfile struct {
	PrimaryAttribute                float32
	IsPrimaryAttributeFound         bool
	PrimaryAttributeBaseline        float32
	IsPrimaryAttributeBaselineFound bool
	BasicDefenseBoost               float32
	PhysicalDefenseBoost            float32
	EnergyDefenseBoost              float32
	DamageBuff                      float32
	ScienceDamage                   [5]float32
	ProjectileDamage                float32
	EnergyDamageBuff                float32
	AreaDamage                      float32
	DamageOverTimeIncrease          float32
	DirectAttackDamagePercent       float32
	PhysicalDamage                  float32
	PhysicalAbilityDamageIncrease   float32
	EnergyDamage                    float32
	EnergyAbilityDamageIncrease     float32
	DirectAttackDamage              float32
}

type DamageRange struct {
	Minimum float32
	Maximum float32
}

// ResolveAbilityDamageRange applies sub_9E4E60, sub_9E4F40, and sub_9E4EF0 in
// their recovered order. Critical and target-side defenses are separate stages.
func ResolveAbilityDamageRange(ability AbilityDamage, profile DamageProfile) (DamageRange, error) {
	if !isFiniteDamage(float64(ability.Minimum)) || !isFiniteDamage(float64(ability.Maximum)) ||
		!isFiniteDamage(float64(ability.Coefficient)) || ability.Minimum < 0 ||
		ability.Maximum < ability.Minimum {
		return DamageRange{}, errors.New("invalid ability damage")
	}
	if ability.IsDamageTypeFound && ability.DamageType >= uint8(len(profile.ScienceDamage)) {
		return DamageRange{}, errors.New("invalid damage type")
	}
	profileNumber := []float32{
		profile.PrimaryAttribute, profile.PrimaryAttributeBaseline,
		profile.BasicDefenseBoost, profile.PhysicalDefenseBoost,
		profile.EnergyDefenseBoost, profile.DamageBuff, profile.ProjectileDamage,
		profile.EnergyDamageBuff, profile.AreaDamage, profile.DamageOverTimeIncrease,
		profile.DirectAttackDamagePercent, profile.PhysicalDamage,
		profile.PhysicalAbilityDamageIncrease, profile.EnergyDamage,
		profile.EnergyAbilityDamageIncrease, profile.DirectAttackDamage,
	}
	profileNumber = append(profileNumber, profile.ScienceDamage[:]...)
	for _, number := range profileNumber {
		if !isFiniteDamage(float64(number)) {
			return DamageRange{}, errors.New("invalid damage profile")
		}
	}
	multiplier := float32(1) + profile.DamageBuff
	if ability.IsDescriptorFound && ability.Descriptor&2 != 0 {
		multiplier += profile.BasicDefenseBoost * (profile.PhysicalDefenseBoost + profile.EnergyDefenseBoost)
	}
	if ability.IsDamageTypeFound {
		multiplier += profile.ScienceDamage[ability.DamageType]
	}
	if ability.IsDescriptorFound && ability.Descriptor&8192 != 0 {
		multiplier += profile.ProjectileDamage
	}
	if ability.IsDamageSourceFound && ability.DamageSource == 1 {
		multiplier += profile.EnergyDamageBuff
	}
	if ability.IsDescriptorFound && ability.Descriptor&8 != 0 {
		multiplier += profile.AreaDamage
	}
	if ability.IsDescriptorFound && ability.Descriptor&4 != 0 {
		multiplier += profile.DamageOverTimeIncrease
	} else if ability.IsDescriptorFound {
		multiplier += profile.DirectAttackDamagePercent
	}
	if ability.IsDescriptorFound && ability.Descriptor&64 != 0 {
		multiplier += profile.PhysicalDamage
		if ability.Descriptor&2 == 0 {
			multiplier += profile.PhysicalAbilityDamageIncrease
		}
	}
	if ability.IsDescriptorFound && ability.Descriptor&128 != 0 {
		multiplier += profile.EnergyDamage
		if ability.Descriptor&2 == 0 {
			multiplier += profile.EnergyAbilityDamageIncrease
		}
	}
	minimum := ability.Minimum
	maximum := ability.Maximum
	if profile.IsPrimaryAttributeFound && ability.Coefficient != 0 {
		// Callers with recovered tuning supply the baseline subtracted by
		// sub_9E4E60. Preserve the existing hero balance for other callers.
		baseline := float32(-1)
		if profile.IsPrimaryAttributeBaselineFound {
			baseline = profile.PrimaryAttributeBaseline
		}
		primaryMultiplier := 1 + (profile.PrimaryAttribute-baseline)*ability.Coefficient
		minimum *= primaryMultiplier
		maximum *= primaryMultiplier
	}
	minimum *= multiplier
	maximum *= multiplier
	if ability.IsDescriptorFound && ability.Descriptor&4 == 0 && ability.Descriptor&4096 == 0 {
		minimum += profile.DirectAttackDamage
		maximum += profile.DirectAttackDamage
	}
	minimum = max(0, minimum)
	maximum = max(0, maximum)
	return DamageRange{Minimum: float32(math.Floor(float64(minimum))), Maximum: float32(math.Ceil(float64(maximum)))}, nil
}

func damageProfile(primaryAttribute float32, isPrimaryAttributeFound bool, attribute [partAttributeCount]float32) DamageProfile {
	return DamageProfile{
		PrimaryAttribute: primaryAttribute, IsPrimaryAttributeFound: isPrimaryAttributeFound,
		BasicDefenseBoost: attribute[16], PhysicalDefenseBoost: attribute[7], EnergyDefenseBoost: attribute[9],
		DamageBuff: attribute[13], ProjectileDamage: attribute[99], EnergyDamageBuff: attribute[28],
		AreaDamage: attribute[37], DamageOverTimeIncrease: attribute[85],
		DirectAttackDamagePercent: attribute[109], PhysicalDamage: attribute[88],
		PhysicalAbilityDamageIncrease: attribute[89], EnergyDamage: attribute[90],
		EnergyAbilityDamageIncrease: attribute[91], DirectAttackDamage: attribute[108],
		ScienceDamage: [5]float32{
			attribute[38], attribute[39], attribute[40], attribute[41], attribute[42],
		},
	}
}

func isFiniteDamage(number float64) bool {
	return !math.IsNaN(number) && !math.IsInf(number, 0)
}
