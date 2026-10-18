package game

import (
	"errors"
	"fmt"
	"math"
)

// DNADropTuning contains immutable authored frequency settings, separate from
// the amount calculation and the server's party-sharing policy.
type DNADropTuning struct {
	Chance       float32
	MinimumStage uint32
}

func (e DNADropTuning) Validate() error {
	if e.Chance <= 0 || e.Chance > 1 ||
		math.IsNaN(float64(e.Chance)) || math.IsInf(float64(e.Chance), 0) {
		return errors.New("DNA drop chance must be finite and in (0, 1]")
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
