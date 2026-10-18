package game

import (
	"errors"
	"fmt"
)

// MagicNumbers.MagicNumbers offsets 68 and 72, exposed by build 103's
// nTuning.DodgePercentCap and nTuning.ResistPercentCap, are both 0.8.
const (
	resonanceDodgeChanceCap  = float32(0.8)
	resonanceResistChanceCap = float32(0.8)
)

func resolveDefenseDamageBonus(profile DamageProfile) (float32, error) {
	if profile.BasicDefenseBoost == 0 {
		return 0, nil
	}
	if profile.DefenseRatingPerPercent <= 0 {
		return 0, errors.New("missing defense rating conversion")
	}
	dodgePercent := min(
		max(float32(0), profile.PhysicalDefenseBoost)/profile.DefenseRatingPerPercent,
		resonanceDodgeChanceCap*100,
	)
	resistPercent := min(
		max(float32(0), profile.EnergyDefenseBoost)/profile.DefenseRatingPerPercent,
		resonanceResistChanceCap*100,
	)
	// The authored coefficient is 0.01 for 100% of the combined chances:
	// ratings become percentage points above, so do not divide by 100 again.
	return (dodgePercent + resistPercent) * profile.BasicDefenseBoost, nil
}

// ConfigureDefenseDamage selects the same difficulty-indexed conversion exposed
// by nTuning.DefenseRatingPerPercent. Reconfigure after changing the mission or
// rebuilding the selected squad so Resonance never retains the previous stage.
func (e *GameplayBinding) ConfigureDefenseDamage(conversions []float32) error {
	if e == nil {
		return errors.New("nil gameplay binding")
	}
	isDefenseDamageActive := false
	for _, creature := range e.Creatures {
		isDefenseDamageActive = isDefenseDamageActive || creature.DamageProfile.BasicDefenseBoost != 0
	}
	for _, creature := range e.ActivatedCreatures {
		isDefenseDamageActive = isDefenseDamageActive || creature.DamageProfile.BasicDefenseBoost != 0
	}
	if e.Difficulty == 0 || e.Difficulty > uint32(len(conversions)) {
		if !isDefenseDamageActive {
			return nil
		}
		return fmt.Errorf("defenseDifficulty: %d", e.Difficulty)
	}
	conversion := conversions[e.Difficulty-1]
	if !isFiniteDamage(float64(conversion)) || conversion <= 0 {
		if !isDefenseDamageActive {
			return nil
		}
		return fmt.Errorf("defenseConversion: %g", conversion)
	}
	for index := range e.Creatures {
		e.Creatures[index].DamageProfile.DefenseRatingPerPercent = conversion
	}
	e.ActivatedCreatures = append([]GameplayCreature(nil), e.ActivatedCreatures...)
	for index := range e.ActivatedCreatures {
		e.ActivatedCreatures[index].DamageProfile.DefenseRatingPerPercent = conversion
	}
	return nil
}
