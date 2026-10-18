package ability

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

// ProjectChannelDrainTiming keeps the drain window separate from warmup and
// recovery. NecroRandom's Lua uses six one-second intervals, then its end
// animation; cooldown reduction must not shorten that channel window.
func ProjectChannelDrainTiming(
	creature game.GameplayCreature, definition sim.AbilityDefinition,
) (sim.AbilityDefinition, error) {
	if definition.Kind != sim.AbilityKindChannelDrain ||
		definition.TickDuration <= 0 || definition.NumberOfTicks == 0 {
		return sim.AbilityDefinition{}, errors.New("invalid channel drain timing")
	}
	projected, err := projectChannelTickTiming(creature, definition)
	if err != nil {
		return sim.AbilityDefinition{}, fmt.Errorf("drainTiming: %w", err)
	}
	return projected, nil
}

// Reconstruct uses six half-second intervals after its warmup, with the same
// ChannelTimeDecrease scaling as Lifeforce Siphon, independent of cooldown.
func ProjectHealingChannelTiming(
	creature game.GameplayCreature, definition sim.AbilityDefinition,
) (sim.AbilityDefinition, error) {
	if definition.Kind != sim.AbilityKindHealingTicks || definition.Name != "FieldMedicSupport" ||
		definition.TickDuration <= 0 || definition.NumberOfTicks == 0 {
		return sim.AbilityDefinition{}, errors.New("invalid healing channel timing")
	}
	projected, err := projectChannelTickTiming(creature, definition)
	if err != nil {
		return sim.AbilityDefinition{}, fmt.Errorf("healingTiming: %w", err)
	}
	return projected, nil
}

func projectChannelTickTiming(
	creature game.GameplayCreature, definition sim.AbilityDefinition,
) (sim.AbilityDefinition, error) {
	recovery := definition.ReleaseDelay - definition.HitDelay -
		time.Duration(definition.NumberOfTicks)*definition.TickDuration
	if recovery < 0 {
		return sim.AbilityDefinition{}, errors.New("invalid channel recovery")
	}
	projected := definition
	var err error
	projected.Cooldown, err = ProjectCooldown(creature, definition)
	if err != nil {
		return sim.AbilityDefinition{}, fmt.Errorf("channelCooldown: %w", err)
	}
	projected.TickDuration, err = game.ResolveChannelDuration(
		definition.TickDuration, creature.TimingProfile,
	)
	if err != nil {
		return sim.AbilityDefinition{}, fmt.Errorf("channelInterval: %w", err)
	}
	// The recovered script clamps each interval to at least 0.05 seconds.
	projected.TickDuration = max(50*time.Millisecond, projected.TickDuration)
	projected.Duration = time.Duration(projected.NumberOfTicks) * projected.TickDuration
	projected.ReleaseDelay = projected.HitDelay + projected.Duration + recovery
	return projected, nil
}
