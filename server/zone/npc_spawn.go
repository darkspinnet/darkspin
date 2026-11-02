package zone

import (
	"errors"
	"fmt"

	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

// RecordNPCSpawn keeps authoritative bookkeeping and reconnect knowledge while
// the caller's raw publication owns live delivery. It adds no transient event.
func (e *Zone) RecordNPCSpawn(spawn zoneprojection.NPCSpawn) error {
	err := e.recordNPCSpawnObjectives(spawn)
	if err != nil {
		return fmt.Errorf("ownedSpawnObjectives: %w", err)
	}
	e.projection.RecordNPCSpawn(spawn)
	return nil
}

func (e *Zone) recordNPCSpawnObjectives(spawn zoneprojection.NPCSpawn) error {
	if e == nil {
		return errors.New("npc spawn zone unavailable")
	}
	if e.info.ObjectiveProgress == nil {
		return nil
	}
	objectIDs := make([]uint32, 0, len(spawn.Plans))
	for _, plan := range spawn.Plans {
		if !plan.IsFixture && !plan.IsEncounterAuxiliary {
			objectIDs = append(objectIDs, plan.ObjectID)
		}
	}
	err := e.info.ObjectiveProgress.Register(objectIDs)
	if err != nil {
		return fmt.Errorf("spawnRegister: %w", err)
	}
	return nil
}
