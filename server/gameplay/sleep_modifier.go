package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

// Each accepted sleep retains the authored damage callback until this exact
// modifier exits, wakes or is retired with its cloud.
func (e heroAuraAreaSchedule) applySleepingCloudTarget(objectID, instanceID uint32, expiresAt time.Time) (*sim.LuaSleepingCloudModifier, bool, error) {
	modifier, err := sim.NewLuaSleepingCloudModifier(e.runtime.program.SleepingCloud)
	if err != nil {
		return nil, false, fmt.Errorf("sleepCallback: %w", err)
	}
	isApplied, err := e.run.npc.ApplyAuraSleep(objectID, instanceID, expiresAt, luaAuraAdmission{
		modifier: modifier, isChainGame: e.binding.Mode == game.ModeChain,
	})
	if err != nil {
		modifier.Stop()
		return nil, false, fmt.Errorf("sleepApply: %w", err)
	}
	if !isApplied {
		modifier.Stop()
		return nil, false, nil
	}
	return modifier, true, nil
}
