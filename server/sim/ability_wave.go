package sim

import (
	"errors"
	"fmt"
	"time"
)

// ProjectileWaveDefinition retains the child hit callback's operands. Width
// is its full three-dimensional distance limit, separate from collision shape.
type ProjectileWaveDefinition struct {
	MinimumWidth   float32
	MaximumWidth   float32
	GrowTime       time.Duration
	ExitEffectName string
}

func projectileWaveDefinitionFromLua(table *luaTable, definition AbilityDefinition) (AbilityDefinition, error) {
	isPiercing, err := luaAbilityBoolean(table, "piercing")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("piercing: %w", err)
	}
	wave := ProjectileWaveDefinition{}
	wave.MinimumWidth, err = luaAbilityRankFloat32(table, "minWidth", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("minimumWidth: %w", err)
	}
	wave.MaximumWidth, err = luaAbilityRankFloat32(table, "maxWidth", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("maximumWidth: %w", err)
	}
	growTime, err := luaAbilityRankNumber(table, "growTime", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("growTime: %w", err)
	}
	wave.GrowTime = luaSeconds(growTime)
	wave.ExitEffectName, err = luaAbilityString(table, "onExitHitEvent")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("exitEffect: %w", err)
	}
	if !isPiercing || wave.MinimumWidth <= 0 || wave.MaximumWidth < wave.MinimumWidth || wave.GrowTime <= 0 {
		return AbilityDefinition{}, errors.New("invalid projectile wave operands")
	}
	definition.IsPiercing = isPiercing
	definition.ProjectileWave = &wave
	return definition, nil
}

func (e ProjectileWaveDefinition) Width(elapsed time.Duration) float32 {
	// Common Lua chunk827, prototype0.19: Lerp deliberately extrapolates.
	progress := float32(float64(elapsed) / float64(e.GrowTime))
	return (1-progress)*e.MinimumWidth + progress*e.MaximumWidth
}
