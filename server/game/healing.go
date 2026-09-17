package game

import (
	"errors"
	"math"
)

// AbilityHealing describes one authored healing amount consumed by build-103's
// sub_9E5550 projection.
type AbilityHealing struct {
	Amount            float32
	Coefficient       float32
	Descriptor        uint32
	IsDescriptorFound bool
}

// HealingProfile is the authenticated healer snapshot used by sub_9E5550 and
// its sub_9E5500 modifier stage.
type HealingProfile struct {
	PrimaryAttribute        float32
	IsPrimaryAttributeFound bool
	HealingIncrease         float32
	HealingOverTimeIncrease float32
}

// HealingTargetProfile is the authenticated recipient snapshot consumed after
// healer-side projection and any critical-healing stage.
type HealingTargetProfile struct {
	HealingReduction float32
}

// ResolveAbilityHealing applies the primary-stat coefficient followed by the
// general healing and descriptor-gated healing-over-time multipliers. Native
// sub_9E5550 returns a float, so authoritative combat retains its fractional
// result instead of applying the Hero Profile page's integer presentation.
func ResolveAbilityHealing(ability AbilityHealing, profile HealingProfile) (float32, error) {
	abilityNumber := []float32{ability.Amount, ability.Coefficient}
	for _, number := range abilityNumber {
		if !isFiniteDamage(float64(number)) || number < 0 {
			return 0, errors.New("invalid ability healing")
		}
	}
	profileNumber := []float32{
		profile.PrimaryAttribute, profile.HealingIncrease, profile.HealingOverTimeIncrease,
	}
	for _, number := range profileNumber {
		if math.IsNaN(float64(number)) || math.IsInf(float64(number), 0) {
			return 0, errors.New("invalid healing profile")
		}
	}
	healing := ability.Amount
	if profile.IsPrimaryAttributeFound && ability.Coefficient != 0 {
		healing *= 1 + (profile.PrimaryAttribute+1)*ability.Coefficient
	}
	multiplier := float32(1) + profile.HealingIncrease
	if ability.IsDescriptorFound && ability.Descriptor&4096 != 0 {
		multiplier += profile.HealingOverTimeIncrease
	}
	healing *= multiplier
	return max(0, healing), nil
}

// ApplyTargetHealingReduction implements build-103 sub_9E5D10's recipient
// attribute-30 stage. The native path applies this multiplier immediately
// before adding the result to current HP.
func ApplyTargetHealingReduction(healing float32, profile HealingTargetProfile) (float32, error) {
	if math.IsNaN(float64(healing)) || math.IsInf(float64(healing), 0) || healing < 0 {
		return 0, errors.New("invalid target healing")
	}
	if math.IsNaN(float64(profile.HealingReduction)) || math.IsInf(float64(profile.HealingReduction), 0) {
		return 0, errors.New("invalid healing target profile")
	}
	return max(0, healing*(1-profile.HealingReduction)), nil
}

func healingProfile(
	primaryAttribute float32, isPrimaryAttributeFound bool, attribute [partAttributeCount]float32,
) HealingProfile {
	return HealingProfile{
		PrimaryAttribute: primaryAttribute, IsPrimaryAttributeFound: isPrimaryAttributeFound,
		HealingIncrease: attribute[96], HealingOverTimeIncrease: attribute[98],
	}
}

func healingTargetProfile(attribute [partAttributeCount]float32) HealingTargetProfile {
	return HealingTargetProfile{HealingReduction: attribute[30]}
}
