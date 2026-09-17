package game

import (
	"errors"
	"math"
)

// ResolveAbilityManaCost implements build-103 sub_9DD720. The Lua-authored
// cost is adjusted only when it is nonzero; an overdrive-charged actor pays
// nothing. Property and coefficient are server-owned content operands.
func ResolveAbilityManaCost(base, property, coefficient float32, isOverdriveCharged bool) (float32, error) {
	number := []float32{base, property, coefficient}
	for _, current := range number {
		if math.IsNaN(float64(current)) || math.IsInf(float64(current), 0) {
			return 0, errors.New("invalid mana cost")
		}
	}
	if isOverdriveCharged || base == 0 {
		return 0, nil
	}
	cost := base + property*coefficient
	if math.IsNaN(float64(cost)) || math.IsInf(float64(cost), 0) {
		return 0, errors.New("invalid mana cost")
	}
	return cost, nil
}
