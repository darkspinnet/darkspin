package sim

import (
	"fmt"
	"time"
)

// ChainLightningDefinition carries the operands of OnHitTarget calls to
// TargetUtils.ChainHitTargets in Chain Lightning and Arc Weld.
type ChainLightningDefinition struct {
	LeapRadius         float32
	UpgradedLeapRadius float32
	MaximumHopCount    uint32
	DamageIncrease     float32
	HopDelay           time.Duration
	EffectNames        []string
}

func loadChainLightning(definition AbilityDefinition, table *luaTable) (AbilityDefinition, error) {
	var err error
	chain := ChainLightningDefinition{}
	chain.LeapRadius, err = luaAbilityRankFloat32(table, "chainLeapRadius", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("chainRadius: %w", err)
	}
	chain.UpgradedLeapRadius, err = luaAbilityRankFloat32(table, "chainLeapRadius", 2)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("chainUpgrade: %w", err)
	}
	chain.MaximumHopCount, err = luaAbilityUint32(table, "maxChains")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("chainCount: %w", err)
	}
	chain.DamageIncrease, err = luaAbilityRankFloat32(table, "damageIncreasePerJump", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("chainDamage: %w", err)
	}
	delay, err := luaAbilityRankNumber(table, "timeToNextJump", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("chainDelay: %w", err)
	}
	chain.HopDelay = time.Duration(delay * float64(time.Second))
	chain.EffectNames, err = luaAbilityStrings(table, "chainEffects")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("chainEffects: %w", err)
	}
	definition.ChainLightning = chain
	return definition, nil
}
