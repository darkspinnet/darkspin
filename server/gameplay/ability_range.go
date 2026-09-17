package gameplay

import (
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

func heroAbilityAdmissionRange(
	creature game.GameplayCreature, definition sim.AbilityDefinition,
) float32 {
	return game.AbilityAdmissionRange(
		definition.Range, creature.RangeIncrease, definition.DescriptorMask,
		definition.IsDescriptorFound,
	)
}
