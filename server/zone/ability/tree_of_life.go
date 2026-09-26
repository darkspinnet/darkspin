package ability

import (
	"errors"
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
)

// ProjectTreeOfLife keeps the tree alive for eight minutes from its spawn.
// Growth stage five is the packaged full-size growth effect. Starting it once
// avoids restarting the growth animation and its sound on each healing tick.
func ProjectTreeOfLife(definition sim.AbilityDefinition) (sim.AbilityDefinition, error) {
	if definition.Name != "TreeOfLife" || definition.Kind != sim.AbilityKindAreaHealing ||
		len(definition.GrowthEffectNames) != 5 || definition.GrowthEffectNames[4] == "" {
		return sim.AbilityDefinition{}, errors.New("invalid Tree of Life definition")
	}
	definition.Duration = 8 * time.Minute
	definition.GrowthEffectNames = []string{definition.GrowthEffectNames[4]}
	return definition, nil
}
