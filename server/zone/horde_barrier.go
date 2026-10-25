package zone

import (
	zonebarrier "github.com/darkspinnet/darkspin/server/zone/barrier"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
)

// OpenHordeBarrierPlans retains the authored open placements after completion.
// Server movement gates are already disabled by the horde's terminal phase.
func (e *Zone) OpenHordeBarrierPlans() [][]zonebarrier.Plan {
	if e == nil || e.info.Horde == nil {
		return nil
	}
	planSets := make([][]zonebarrier.Plan, 0)
	for _, snapshot := range e.info.Horde.Snapshots() {
		if snapshot.Phase != zonehorde.PhaseComplete || snapshot.IsGateActive {
			continue
		}
		plans := e.HordeBarrierPlans(snapshot.MarkerSetName)
		if len(plans) != 0 {
			planSets = append(planSets, plans)
		}
	}
	return planSets
}
