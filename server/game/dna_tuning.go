package game

import (
	"errors"
	"fmt"
	"math"
)

// DNADropTuning contains immutable authored frequency and amount settings.
type DNADropTuning struct {
	Chance          float32
	MinimumStage    uint32
	AmountScalar    float32
	FirstGrowthBase float32
	AmountFactors   [2]float32
}

func (e DNADropTuning) Validate() error {
	if e.Chance <= 0 || e.Chance > 1 ||
		math.IsNaN(float64(e.Chance)) || math.IsInf(float64(e.Chance), 0) {
		return errors.New("DNA drop chance must be finite and in (0, 1]")
	}
	if e.AmountScalar <= 0 || math.IsNaN(float64(e.AmountScalar)) || math.IsInf(float64(e.AmountScalar), 0) {
		return errors.New("DNA amount scalar must be finite and positive")
	}
	if e.FirstGrowthBase <= 0 || math.IsNaN(float64(e.FirstGrowthBase)) || math.IsInf(float64(e.FirstGrowthBase), 0) {
		return errors.New("DNA growth base must be finite and positive")
	}
	for _, factor := range e.AmountFactors {
		if factor < 0 || math.IsNaN(float64(factor)) || math.IsInf(float64(factor), 0) {
			return errors.New("DNA amount factor must be finite and nonnegative")
		}
	}
	if e.AmountFactors[1] < e.AmountFactors[0] {
		return errors.New("DNA amount factor range reversed")
	}
	return nil
}

func (e DNADropTuning) IsDrop(randomDraw float64) (bool, error) {
	err := e.Validate()
	if err != nil {
		return false, fmt.Errorf("dnaTuning: %w", err)
	}
	if randomDraw < 0 || randomDraw >= 1 ||
		math.IsNaN(randomDraw) || math.IsInf(randomDraw, 0) {
		return false, errors.New("invalid DNA probability draw")
	}
	// The native emitter rejects only draws strictly greater than the chance.
	return randomDraw <= float64(e.Chance), nil
}
