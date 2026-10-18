package sim

import (
	"errors"
	"math"
)

// OrbDropBudget is the native random-[0,100) selection budget after applying
// the difficulty-indexed scale to the source amount. GuaranteedSelection and
// RemainderThreshold size output slots; guaranteed attempts still consume draws.
type OrbDropBudget struct {
	ScaledBudget        uint32
	GuaranteedSelection uint32
	RemainderThreshold  uint32
}

func PlanOrbDropBudget(sourceAmount int32, difficultyScale float32) (OrbDropBudget, error) {
	if sourceAmount <= 0 || difficultyScale < 0 ||
		math.IsNaN(float64(difficultyScale)) || math.IsInf(float64(difficultyScale), 0) {
		return OrbDropBudget{}, errors.New("invalid orb drop input")
	}
	// Native CVTSI2SS/MULSS stores the product in float32 before CVTTSS2SI.
	// Keep that rounding boundary: using float64 can turn a budget of 100
	// into 99 and incorrectly make its last capsule a fractional attempt.
	scaledAmount := float32(sourceAmount) * difficultyScale
	if math.IsInf(float64(scaledAmount), 0) || scaledAmount >= float32(1<<31) {
		return OrbDropBudget{}, errors.New("orb drop budget overflow")
	}
	// Native conversion truncates the scaled challenge before any attempt.
	budget := uint32(int32(scaledAmount))
	return OrbDropBudget{
		ScaledBudget:        budget,
		GuaranteedSelection: budget / 100,
		RemainderThreshold:  budget % 100,
	}, nil
}

// CrystalDropThreshold returns native percentage points for an Index(100)
// draw. The baseline truncates before comparison; gameplay pity is separate.
func CrystalDropThreshold(sourceAmount int32, chanceScale float32) (float32, error) {
	if sourceAmount <= 0 || chanceScale < 0 ||
		math.IsNaN(float64(chanceScale)) || math.IsInf(float64(chanceScale), 0) {
		return 0, errors.New("invalid crystal drop input")
	}
	threshold := float32(sourceAmount) * float32(0.15)
	threshold *= chanceScale
	if math.IsInf(float64(threshold), 0) || threshold >= float32(1<<31) {
		return 0, errors.New("crystal drop threshold overflow")
	}
	return float32(int32(threshold)), nil
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
