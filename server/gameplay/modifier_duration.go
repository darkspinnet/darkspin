package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

// projectRecipientModifierDuration projects a verified native modifier
// duration, not a Lua wait or aura lifetime. Descriptors belong to the modifier
// definition, which can differ from the ability that requests it. This covers
// the recovered own-attribute stage; diminishing-return history is separate.
func projectRecipientModifierDuration(duration time.Duration, descriptors uint32,
	creature game.GameplayCreature,
) (time.Duration, error) {
	projected, err := game.ResolveModifierAttributeDuration(game.ModifierTiming{
		Duration: duration, IsBuff: descriptors&16 != 0,
		IsDebuff: descriptors&32 != 0, IsDamageOverTime: descriptors&4 != 0,
	}, game.ModifierTimingProfile{
		BuffDurationIncrease: creature.PartAttribute[50], DebuffDurationReduction: creature.PartAttribute[51],
		DamageOverTimeReduction: creature.PartAttribute[94], CrowdControlTimeReduction: creature.PartAttribute[93],
	})
	if err != nil {
		return 0, fmt.Errorf("modifierDuration: %w", err)
	}
	return projected, nil
}
