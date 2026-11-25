package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/zone"
)

// The registry lock is held. Dispel retires accepted child handles, not the
// field's authored contact history. Each removal is queued before proceeding
// to another fallible child so a later error cannot lose accepted deletion.
func (e *gameplaySessionRegistry) retireTargetAuraModifiersLocked(world *zone.Zone, objectID uint32) (bool, error) {
	if world == nil || objectID == 0 {
		return false, nil
	}
	isPurged := false
	for run := range e.auraRuns {
		if run.originalZone != world || run.npc != world.NPCs() || run.arenaGameID != 0 {
			continue
		}
		switch run.statusKind {
		case sim.AbilityStatusKindSilence, sim.AbilityStatusKindSleep, sim.AbilityStatusKindSlow:
		default:
			continue
		}
		run.mutex.Lock()
		target, isTracked := run.targets[objectID]
		if run.isCleaned || !isTracked {
			run.mutex.Unlock()
			continue
		}
		packets, err := marshalAuraAreaDeletes([]heroAuraAreaTargetDelete{{objectID: objectID, instanceID: target.instanceID}})
		if err != nil {
			run.mutex.Unlock()
			return isPurged, fmt.Errorf("auraDispelEncode: %w", err)
		}
		run.queuePresentationLocked(packets, true)
		run.clearAuraTargetStatus(objectID, target)
		delete(run.targets, objectID)
		if run.isLuaAura {
			if run.retiredTargets == nil {
				run.retiredTargets = make(map[uint32]uint32)
			}
			// Bounded by the retained Lua contact map. Its eventual exit or
			// CleanupAuras consumes this old handle without releasing it twice.
			run.retiredTargets[objectID] = target.instanceID
		}
		run.releaseAuraInstance(target.instanceID)
		run.mutex.Unlock()
		isPurged = true
	}
	return isPurged, nil
}
