package sim

import (
	"fmt"
	"time"
)

// Web Transfusion owns its channel loop instead of using a melee hit. The
// packaged callback waits once after every pulse, including the last pulse.
func webTransfusionDefinition(table *luaTable, definition AbilityDefinition) (AbilityDefinition, error) {
	definition.Kind = AbilityKindChannelDrain
	var err error
	definition.MinimumDamage, definition.MaximumDamage, err = luaAbilityDamage(table, 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("webDamage: %w", err)
	}
	definition.DamageCoefficient, err = luaAbilityRankFloat32(table, "damageCoefficient", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("webCoefficient: %w", err)
	}
	definition.Radius, err = luaAbilityRankFloat32(table, "radius", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("webRadius: %w", err)
	}
	definition.LifeSteal, err = luaAbilityRankFloat32(table, "lifesteal", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("webLifesteal: %w", err)
	}
	tickCount, err := luaAbilityRankNumber(table, "ticks", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("webTicks: %w", err)
	}
	if tickCount <= 0 || tickCount > float64(^uint32(0)) || float64(uint32(tickCount)) != tickCount {
		return AbilityDefinition{}, fmt.Errorf("webTicks: invalid")
	}
	definition.NumberOfTicks = uint32(tickCount)
	interval, err := luaAbilityRankNumber(table, "tickBetweenTicks", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("webInterval: %w", err)
	}
	definition.TickDuration = luaSeconds(interval)
	definition.Duration = time.Duration(definition.NumberOfTicks) * definition.TickDuration
	definition.ReleaseDelay = definition.HitDelay + definition.Duration
	definition.ReleaseDelays = []time.Duration{definition.ReleaseDelay}
	definition.OutAnimationName, err = luaAbilityString(table, "relax_animation")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("webRelax: %w", err)
	}
	definition.MuzzleEffectName, err = luaAbilityString(table, "ribbonEffect")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("webRibbon: %w", err)
	}
	definition.HitEffectName, err = luaAbilityString(table, "sparkEffect")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("webSpark: %w", err)
	}
	definition.ActivationEffectName, err = luaAbilityString(table, "aoeEffect")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("webEffect: %w", err)
	}
	definition.DescriptorMask, definition.IsDescriptorFound, err = luaAbilityOptionalUint32(table, "descriptors")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("webDescriptor: %w", err)
	}
	definition.DamageType, definition.IsDamageTypeFound, err = luaAbilityOptionalUint32(table, "damageType")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("webType: %w", err)
	}
	definition.DamageSource, definition.IsDamageSourceFound, err = luaAbilityOptionalUint32(table, "damageSource")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("webSource: %w", err)
	}
	definition.IsAreaRadiusScaled = true
	return definition, nil
}
