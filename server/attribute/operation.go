// Package attribute evaluates build-103 attribute modifier contributions.
package attribute

import (
	"errors"
	"fmt"
	"math"
)

type Operation uint8

const (
	Add               Operation = 1
	PercentChange     Operation = 2
	Clamp             Operation = 3
	AddBest           Operation = 4
	PercentChangeBest Operation = 5
)

type Contribution struct {
	Handle    uint32
	Operation Operation
	Amount    float32
}

// Resolve applies native build-103 0x9E2680's operation order to a baseline
// supplied by the owning feature. Contributions must preserve insertion order.
// Handles identify owned records; zero is allowed for untracked contributions.
func Resolve(base float32, contributions []Contribution) (float32, error) {
	if !isFinite(base) {
		return 0, errors.New("invalid attribute baseline")
	}
	if len(contributions) == 0 {
		return base, nil
	}
	add := float32(0)
	percent := float32(0)
	upper := float32(math.MaxFloat32)
	addBest := float32(0)
	percentBest := float32(0)
	for index, contribution := range contributions {
		if !isFinite(contribution.Amount) {
			return 0, fmt.Errorf("invalid attribute amount[%d]", index)
		}
		switch contribution.Operation {
		case Add:
			add += contribution.Amount
		case PercentChange:
			percent += contribution.Amount
		case Clamp:
			upper = min(upper, contribution.Amount)
		case AddBest:
			addBest = max(addBest, contribution.Amount)
		case PercentChangeBest:
			percentBest = max(percentBest, contribution.Amount)
		default:
			return 0, fmt.Errorf("invalid attribute operation[%d]: %d", index, contribution.Operation)
		}
	}
	aggregate := base + add
	if percent != 0 {
		scale := float32(1) + percent
		aggregate *= scale
	}
	if addBest != 0 {
		aggregate += addBest
	}
	if percentBest != 0 {
		scale := float32(1) + percentBest
		aggregate *= scale
	}
	aggregate = min(aggregate, upper)
	if !isFinite(aggregate) {
		return 0, errors.New("invalid attribute aggregate")
	}
	return aggregate, nil
}

func isFinite(amount float32) bool {
	return !math.IsNaN(float64(amount)) && !math.IsInf(float64(amount), 0)
}
