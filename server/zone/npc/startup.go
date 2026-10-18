package npc

import (
	"strings"

	"github.com/darkspinnet/darkspin/server/sim"
)

// BindStartupRandom shares the zone's checkpointed simulator stream. Native
// A28070 (127) draws once for positive useSecondaryStart, never per observer.
func (e *Session) BindStartupRandom(random *sim.SimulatorRandom) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.startupRandom = random
}

// PrepareIntroductions resolves idle presentation before admission AND packet
// construction. Its returned plans must be used for both, preserving one choice
// for all peers. Already prepared/restored plans consume no additional draws.
func (e *Session) PrepareIntroductions(plans []SpawnPlan) []SpawnPlan {
	preparedPlans := make([]SpawnPlan, len(plans))
	if e == nil {
		copy(preparedPlans, plans)
		return preparedPlans
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for index, plan := range plans {
		preparedPlans[index] = e.prepareIntroduction(plan)
	}
	return preparedPlans
}

func (e *Session) prepareIntroduction(plan SpawnPlan) SpawnPlan {
	if plan.IsStartupInitialized || plan.OwnerObjectID != 0 || plan.IsFixture {
		return plan
	}
	graph := plan.NPCProfile.AIGraph
	if graph == nil || !graph.IsResolved {
		return plan
	}
	plan.IsStartupInitialized = true
	if graph.UseSecondaryStart > 0 && e.startupRandom != nil {
		plan.IsSecondaryStart = float64(graph.UseSecondaryStart) > e.startupRandom.Float64()
	}
	idle := graph.PreAggroIdle
	if plan.IsSecondaryStart {
		idle = graph.PreAggroIdle2
	}
	// Floor-warp encounters have their own reveal timeline and never enter the
	// pre-aggro idle. Keep that presentation independent of ambient startup.
	plan.IsIntroductionHidden = plan.Introduction != SpawnIntroductionFloorWarp &&
		strings.EqualFold(idle, "nBehavior_Invisible")
	return plan
}
