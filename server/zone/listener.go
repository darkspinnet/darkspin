package zone

import (
	"strings"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// DestroyHordeBarrierListeners accompanies authoritative barrier deletion.
// Defeat alone does not destroy other placement owners or their subscriptions.
func (e *Zone) DestroyHordeBarrierListeners(markerSetName string) {
	if e == nil || e.info.Director == nil {
		return
	}
	for _, set := range e.info.DirectorDefinition.MarkerSets {
		if !strings.EqualFold(set.Name, markerSetName) {
			continue
		}
		for _, plan := range e.HordeBarrierPlans(markerSetName) {
			e.info.Director.RemoveListenerOwner(set.Ordinal, plan.MarkerID())
		}
	}
}

func (e *Zone) destroyDeletedFixtureListeners(deaths []zonenpc.DeathEvent) {
	if e.info.Director == nil || e.info.NPCs == nil {
		return
	}
	for _, death := range deaths {
		if death.Kind != zonenpc.DeathDelete {
			continue
		}
		npc, isFound := e.info.NPCs.NPC(death.TargetObjectID)
		if !isFound || !npc.Plan.IsFixture {
			continue
		}
		for _, set := range e.info.DirectorDefinition.MarkerSets {
			if strings.EqualFold(set.Name, npc.Plan.MarkerSetName) {
				e.info.Director.RemoveListenerOwner(set.Ordinal, npc.Plan.LocusID)
			}
		}
	}
}
