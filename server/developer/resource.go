package developer

import (
	"errors"
	"math"
)

type ResourceCommand struct {
	Damage         float32
	PowerReduction float32
	IsHeal         bool
	IsPowerFill    bool
}

type ResourceState struct {
	HitPoint          float32
	HitPointMaximum   float32
	PowerPoint        float32
	PowerPointMaximum float32
}

type ResourceMutation struct {
	Damage              float32
	HitPoint            float32
	PowerPoint          float32
	IsHitPointChanged   bool
	IsPowerPointChanged bool
}

func ApplyResource(
	state ResourceState, command ResourceCommand,
) (ResourceMutation, error) {
	if !isFiniteNonnegative(state.HitPoint) ||
		!isFinitePositive(state.HitPointMaximum) ||
		!isFiniteNonnegative(state.PowerPoint) ||
		!isFinitePositive(state.PowerPointMaximum) {
		return ResourceMutation{}, errors.New("invalid resource state")
	}
	if command.IsHeal {
		return ResourceMutation{
			HitPoint: state.HitPointMaximum, IsHitPointChanged: true,
		}, nil
	}
	if command.IsPowerFill || command.PowerReduction > 0 {
		powerPoint := state.PowerPointMaximum
		if command.PowerReduction > 0 {
			if !isFinitePositive(command.PowerReduction) {
				return ResourceMutation{}, errors.New("invalid power reduction")
			}
			powerPoint = max(float32(0), state.PowerPoint-command.PowerReduction)
		}
		return ResourceMutation{
			PowerPoint: powerPoint, IsPowerPointChanged: true,
		}, nil
	}
	if !isFinitePositive(command.Damage) {
		return ResourceMutation{}, errors.New("invalid damage")
	}
	damage := min(command.Damage, state.HitPoint)
	return ResourceMutation{
		Damage: damage, HitPoint: max(float32(0), state.HitPoint-damage),
		IsHitPointChanged: true,
	}, nil
}

func isFiniteNonnegative(number float32) bool {
	return number >= 0 && !math.IsNaN(float64(number)) && !math.IsInf(float64(number), 0)
}

func isFinitePositive(number float32) bool {
	return number > 0 && !math.IsNaN(float64(number)) && !math.IsInf(float64(number), 0)
}
