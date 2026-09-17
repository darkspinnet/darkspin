package sim

import (
	"errors"
	"math"
)

// OrbDropBudget is the native random-[0,100) selection budget after applying
// the difficulty-indexed scale to the source amount.
type OrbDropBudget struct {
	GuaranteedSelection uint32
	RemainderThreshold  uint32
}

func PlanOrbDropBudget(sourceAmount int32, difficultyScale float32) (OrbDropBudget, error) {
	if sourceAmount <= 0 || difficultyScale <= 0 ||
		math.IsNaN(float64(difficultyScale)) || math.IsInf(float64(difficultyScale), 0) {
		return OrbDropBudget{}, errors.New("invalid orb drop input")
	}
	scaledAmount := float64(sourceAmount) * float64(difficultyScale)
	if scaledAmount > float64(math.MaxInt32) {
		return OrbDropBudget{}, errors.New("orb drop budget overflow")
	}
	budget := uint32(int32(scaledAmount))
	return OrbDropBudget{
		GuaranteedSelection: budget / 100,
		RemainderThreshold:  budget % 100,
	}, nil
}

func CrystalDropThreshold(sourceAmount int32, chanceScale float32) (float32, error) {
	if sourceAmount <= 0 || chanceScale < 0 ||
		math.IsNaN(float64(chanceScale)) || math.IsInf(float64(chanceScale), 0) {
		return 0, errors.New("invalid crystal drop input")
	}
	return float32(sourceAmount) * 0.15 * chanceScale, nil
}

func EquipmentDropThreshold(
	playerCount int, sourceAmount int32, lootScalar float32, chanceScale float32,
) (float32, error) {
	if playerCount <= 0 || sourceAmount <= 0 || lootScalar < 0 || chanceScale < 0 ||
		math.IsNaN(float64(lootScalar)) || math.IsInf(float64(lootScalar), 0) ||
		math.IsNaN(float64(chanceScale)) || math.IsInf(float64(chanceScale), 0) {
		return 0, errors.New("invalid equipment drop input")
	}
	threshold := float32(playerCount) * float32(sourceAmount) * lootScalar * 0.01 * chanceScale
	if math.IsInf(float64(threshold), 0) {
		return 0, errors.New("equipment drop threshold overflow")
	}
	return threshold, nil
}
