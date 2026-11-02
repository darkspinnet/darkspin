package gameplay

import (
	"errors"
	"fmt"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// A producer that already owns initial death bytes still records authoritative
// state and objectives, without adding a competing semantic wire publication.
// Later corpse deadlines keep the ordinary RecordNPCDeaths path.
func (e gameplayProjectionRuntime) recordEnemyDeathObjectives(
	sessionKey string, generation uint64, deaths []zonenpc.DeathEvent,
) error {
	if len(deaths) == 0 {
		return nil
	}
	if e.registry == nil || sessionKey == "" || generation == 0 {
		return errors.New("owned death recording unavailable")
	}
	e.registry.mutex.RLock()
	member, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && member.generation == generation && member.zone != nil
	world := member.zone
	e.registry.mutex.RUnlock()
	if !isCurrent {
		return errors.New("owned death world replaced")
	}
	err := world.RecordNPCDeathObjectives(world.Context(), deaths)
	if err != nil {
		return fmt.Errorf("ownedDeathObjectives: %w", err)
	}
	return nil
}
