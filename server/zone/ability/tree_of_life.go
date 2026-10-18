package ability

import (
	"errors"

	"github.com/darkspinnet/darkspin/server/sim"
)

// ProjectTreeOfLife presents the packaged full-size growth effect once while
// retaining the authored lifetime and expiration cleanup.
func ProjectTreeOfLife(definition sim.AbilityDefinition) (sim.AbilityDefinition, error) {
	if definition.Name != "TreeOfLife" || definition.Kind != sim.AbilityKindAreaHealing ||
		len(definition.GrowthEffectNames) != 5 || definition.GrowthEffectNames[4] == "" {
		return sim.AbilityDefinition{}, errors.New("invalid Tree of Life definition")
	}
	definition.GrowthEffectNames = []string{definition.GrowthEffectNames[4]}
	return definition, nil
}
