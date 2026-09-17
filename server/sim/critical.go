package sim

import (
	"errors"
	"fmt"
	"math"
)

// CriticalTuning contains the immutable authored operands for critical combat.
type CriticalTuning struct {
	DamageBonus       float32
	RatingConversions []float32
}

// CriticalProfile is one attacker's current critical-combat snapshot.
type CriticalProfile struct {
	Rating         float32
	AutoCrit       float32
	DamageIncrease float32
}

// CriticalDamageResult is the committed damage and critical classification.
type CriticalDamageResult struct {
	Damage     float32
	IsCritical bool
}

// ResolveCriticalDamage rolls and, on success, applies the authored critical
// multiplier. Validation completes before the random stream is advanced.
func ResolveCriticalDamage(
	random *SimulatorRandom, damage float32, difficulty uint32,
	profile CriticalProfile, tuning CriticalTuning,
) (CriticalDamageResult, error) {
	if invalidCriticalNumber(damage) || damage <= 0 {
		return CriticalDamageResult{}, fmt.Errorf("damage: %g", damage)
	}
	if invalidCriticalNumber(tuning.DamageBonus) || tuning.DamageBonus <= 0 {
		return CriticalDamageResult{}, fmt.Errorf("damageBonus: %g", tuning.DamageBonus)
	}
	isCritical, err := RollCritical(
		random, profile.Rating, profile.AutoCrit, difficulty, tuning.RatingConversions,
	)
	if err != nil {
		return CriticalDamageResult{}, fmt.Errorf("criticalRoll: %w", err)
	}
	if !isCritical {
		return CriticalDamageResult{Damage: damage}, nil
	}
	criticalDamage, err := ApplyCriticalDamage(damage, tuning.DamageBonus, profile.DamageIncrease)
	if err != nil {
		return CriticalDamageResult{}, fmt.Errorf("criticalApply: %w", err)
	}
	return CriticalDamageResult{Damage: criticalDamage, IsCritical: true}, nil
}

// ResolveNonPlayerCriticalDamage reproduces the native non-player branch,
// which uses a fixed rating conversion of one instead of DifficultyTuning.
func ResolveNonPlayerCriticalDamage(
	random *SimulatorRandom, damage float32, profile CriticalProfile, tuning CriticalTuning,
) (CriticalDamageResult, error) {
	if invalidCriticalNumber(damage) || damage <= 0 {
		return CriticalDamageResult{}, fmt.Errorf("damage: %g", damage)
	}
	if invalidCriticalNumber(tuning.DamageBonus) || tuning.DamageBonus <= 0 {
		return CriticalDamageResult{}, fmt.Errorf("damageBonus: %g", tuning.DamageBonus)
	}
	isCritical, err := rollCriticalConversion(random, profile.Rating, profile.AutoCrit, 1)
	if err != nil {
		return CriticalDamageResult{}, fmt.Errorf("criticalRoll: %w", err)
	}
	if !isCritical {
		return CriticalDamageResult{Damage: damage}, nil
	}
	criticalDamage, err := ApplyCriticalDamage(damage, tuning.DamageBonus, profile.DamageIncrease)
	if err != nil {
		return CriticalDamageResult{}, fmt.Errorf("criticalApply: %w", err)
	}
	return CriticalDamageResult{Damage: criticalDamage, IsCritical: true}, nil
}

// RollCritical reproduces build-103's AutoCrit and rating-conversion test.
// Difficulty is one-based and selects the authored rating conversion.
func RollCritical(
	random *SimulatorRandom, criticalRating, autoCrit float32,
	difficulty uint32, ratingConversions []float32,
) (bool, error) {
	if invalidCriticalNumber(criticalRating) || criticalRating < 0 {
		return false, fmt.Errorf("criticalRating: %g", criticalRating)
	}
	if invalidCriticalNumber(autoCrit) {
		return false, fmt.Errorf("autoCrit: %g", autoCrit)
	}
	if autoCrit > 0 {
		return true, nil
	}
	if random == nil {
		return false, errors.New("nil simulator random")
	}
	if difficulty == 0 || difficulty > uint32(len(ratingConversions)) {
		return false, fmt.Errorf("difficulty: %d", difficulty)
	}
	conversion := ratingConversions[difficulty-1]
	return rollCriticalConversion(random, criticalRating, autoCrit, conversion)
}

func rollCriticalConversion(
	random *SimulatorRandom, criticalRating, autoCrit, conversion float32,
) (bool, error) {
	if invalidCriticalNumber(criticalRating) || criticalRating < 0 {
		return false, fmt.Errorf("criticalRating: %g", criticalRating)
	}
	if invalidCriticalNumber(autoCrit) {
		return false, fmt.Errorf("autoCrit: %g", autoCrit)
	}
	if autoCrit > 0 {
		return true, nil
	}
	if random == nil {
		return false, errors.New("nil simulator random")
	}
	if invalidCriticalNumber(conversion) || conversion <= 0 {
		return false, fmt.Errorf("ratingConversion: %g", conversion)
	}
	chance := criticalRating / (conversion * 100)
	if chance > 1 {
		chance = 1
	}
	draw := float32(random.Float64())
	return chance > draw, nil
}

// ApplyCriticalDamage applies MagicNumbers.CriticalDamageBonus plus the
// attacker's CriticalDamageIncrease to an already selected damage amount.
func ApplyCriticalDamage(damage, damageBonus, damageIncrease float32) (float32, error) {
	if invalidCriticalNumber(damage) || damage < 0 {
		return 0, fmt.Errorf("damage: %g", damage)
	}
	if invalidCriticalNumber(damageBonus) || damageBonus <= 0 {
		return 0, fmt.Errorf("damageBonus: %g", damageBonus)
	}
	if invalidCriticalNumber(damageIncrease) {
		return 0, fmt.Errorf("damageIncrease: %g", damageIncrease)
	}
	multiplier := damageBonus + damageIncrease
	if multiplier < 0 {
		multiplier = 0
	}
	return damage * multiplier, nil
}

func invalidCriticalNumber(number float32) bool {
	return math.IsNaN(float64(number)) || math.IsInf(float64(number), 0)
}
