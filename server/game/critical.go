package game

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/util"
)

// configurePassiveCritical retains nTuning.CriticalRatingPerCritPercent for
// the mission. Build 103 sub_9E1EB0 indexes DifficultyTuning's rating array
// directly with the one-based mission difficulty, unlike the critical roller's
// difficulty-minus-one lookup. Clamp the final unsupported native array access
// to the last authored row rather than reading past the resource.
func (e *GameplayBinding) configurePassiveCritical(conversions []float32) error {
	isBlitzSelected := false
	for _, creature := range e.Creatures {
		isBlitzSelected = isBlitzSelected || creature.PassiveAbility == util.HashID("LightningRoguePassive")
	}
	for _, creature := range e.ActivatedCreatures {
		isBlitzSelected = isBlitzSelected || creature.PassiveAbility == util.HashID("LightningRoguePassive")
	}
	if !isBlitzSelected {
		return nil
	}
	if e.Difficulty == 0 || e.Difficulty > uint32(len(conversions)) {
		return fmt.Errorf("criticalDifficulty: %d", e.Difficulty)
	}
	conversion := conversions[min(e.Difficulty, uint32(len(conversions)-1))]
	if !isFiniteDamage(float64(conversion)) || conversion <= 0 {
		return fmt.Errorf("criticalConversion: %g", conversion)
	}
	for index := range e.Creatures {
		e.Creatures[index].CriticalRatingPerPercent = conversion
	}
	e.ActivatedCreatures = append([]GameplayCreature(nil), e.ActivatedCreatures...)
	for index := range e.ActivatedCreatures {
		e.ActivatedCreatures[index].CriticalRatingPerPercent = conversion
	}
	return nil
}
