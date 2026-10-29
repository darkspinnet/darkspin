package population

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/sim"
)

func (e *Session) RestoreRandom(snapshot sim.RandomSnapshot, mixedState uint32) error {
	random, err := sim.NewSimulatorRandomFromSnapshot(snapshot)
	if err != nil {
		return fmt.Errorf("populationRandom: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.random = random
	e.mixedRandom.mu.Lock()
	e.mixedRandom.state = mixedState
	e.mixedRandom.mu.Unlock()
	return nil
}

func (e *Session) MixedRandomState() uint32 {
	e.mixedRandom.mu.Lock()
	defer e.mixedRandom.mu.Unlock()
	return e.mixedRandom.state
}
