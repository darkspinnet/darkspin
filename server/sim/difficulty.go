package sim

import (
	"errors"
	"fmt"
	"math"
)

// DifficultyCombatTuning contains the client-proven non-player scaling
// operands. StarIncrement is supplied separately because ordinary campaign
// play initializes it to zero.
type DifficultyCombatTuning struct {
	StarHealthBase   float32
	StarDamageBase   float32
	HealthMultiplier []float32
	DamageMultiplier []float32
}

// ProjectNonPlayerHealth applies the build-103 health tuning stage. Difficulty
// is one-based and values beyond the authored series use its final row.
func ProjectNonPlayerHealth(
	baseHealth float32, difficulty uint32, starIncrement uint32, tuning DifficultyCombatTuning,
) (float32, error) {
	return projectNonPlayerDifficulty(
		baseHealth, difficulty, starIncrement, tuning.StarHealthBase,
		tuning.HealthMultiplier, "health",
	)
}

// ProjectNonPlayerDamage applies the build-103 damage tuning stage. Separate
// captain, elite, and modifier stages are intentionally not included.
func ProjectNonPlayerDamage(
	baseDamage float32, difficulty uint32, starIncrement uint32, tuning DifficultyCombatTuning,
) (float32, error) {
	return projectNonPlayerDifficulty(
		baseDamage, difficulty, starIncrement, tuning.StarDamageBase,
		tuning.DamageMultiplier, "damage",
	)
}

// ProjectNonPlayerHealthForParty adds the exact build-103 player-count health
// stage. The caller supplies the authored coefficient selected by stable match
// membership cardinality.
func ProjectNonPlayerHealthForParty(
	baseHealth float32, difficulty uint32, starIncrement uint32,
	partyCoefficient float32, playerCountHealthScale float32, tuning DifficultyCombatTuning,
) (float32, error) {
	if invalidDifficultyNumber(partyCoefficient) || partyCoefficient < 0 {
		return 0, fmt.Errorf("partyCoefficient: %g", partyCoefficient)
	}
	if invalidDifficultyNumber(playerCountHealthScale) {
		return 0, fmt.Errorf("playerCountHealthScale: %g", playerCountHealthScale)
	}
	partyHealth := baseHealth * (1 + partyCoefficient*playerCountHealthScale)
	return ProjectNonPlayerHealth(partyHealth, difficulty, starIncrement, tuning)
}

// ProjectNonPlayerDamageForParty adds the exact build-103 player-count
// non-player damage/healing scalar. Invalid counts use the neutral scalar.
func ProjectNonPlayerDamageForParty(
	baseDamage float32, difficulty uint32, starIncrement uint32,
	playerCount uint32, tuning DifficultyCombatTuning,
) (float32, error) {
	partyMultiplier := float32(1)
	switch playerCount {
	case 1:
		partyMultiplier = 1
	case 2:
		partyMultiplier = 1.4
	case 3:
		partyMultiplier = 1.8
	case 4:
		partyMultiplier = 2.2
	}
	return ProjectNonPlayerDamage(baseDamage*partyMultiplier, difficulty, starIncrement, tuning)
}

func projectNonPlayerDifficulty(
	baseAmount float32, difficulty uint32, starIncrement uint32,
	starBase float32, multiplier []float32, label string,
) (float32, error) {
	if invalidDifficultyNumber(baseAmount) || baseAmount <= 0 {
		return 0, fmt.Errorf("base%s: %g", label, baseAmount)
	}
	if difficulty == 0 {
		return 0, errors.New("difficulty: zero")
	}
	if invalidDifficultyNumber(starBase) || starBase <= 0 {
		return 0, fmt.Errorf("%sStarBase: %g", label, starBase)
	}
	if len(multiplier) == 0 {
		return 0, fmt.Errorf("%sMultiplier: empty", label)
	}
	index := int(difficulty - 1)
	if index >= len(multiplier) {
		index = len(multiplier) - 1
	}
	selectedMultiplier := multiplier[index]
	if invalidDifficultyNumber(selectedMultiplier) || selectedMultiplier <= 0 {
		return 0, fmt.Errorf("%sMultiplier[%d]: %g", label, index, selectedMultiplier)
	}
	projected := float64(baseAmount) * float64(selectedMultiplier) *
		math.Pow(float64(starBase), float64(starIncrement))
	if math.IsNaN(projected) || math.IsInf(projected, 0) || projected <= 0 || projected > math.MaxFloat32 {
		return 0, fmt.Errorf("projected%s: %g", label, projected)
	}
	return float32(projected), nil
}

func invalidDifficultyNumber(number float32) bool {
	return math.IsNaN(float64(number)) || math.IsInf(float64(number), 0)
}
