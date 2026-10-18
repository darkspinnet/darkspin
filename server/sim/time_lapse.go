package sim

import "fmt"

type TimeLapseDefinition struct {
	DamageCap          float32
	CapCoefficient     float32
	SmallHitEffectName string
}

func loadTimeLapse(definition AbilityDefinition, table *luaTable) (AbilityDefinition, error) {
	var err error
	definition.Kind = AbilityKindPointBlank
	definition.Radius, err = luaAbilityRankFloat32(table, "radius", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("timeLapseRadius: %w", err)
	}
	policy := &TimeLapseDefinition{}
	policy.DamageCap, err = luaAbilityRankFloat32(table, "damageCap", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("timeLapseCap: %w", err)
	}
	policy.CapCoefficient, err = luaAbilityRankFloat32(table, "capCoefficient", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("timeLapseCoefficient: %w", err)
	}
	policy.SmallHitEffectName, err = luaAbilityString(table, "smallHitEffect")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("timeLapseEffect: %w", err)
	}
	definition.TimeLapse = policy
	return definition, nil
}
