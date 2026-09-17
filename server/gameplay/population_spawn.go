package gameplay

import zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"

func campaignPopulationFirstActionPlans(
	spawnPlans []zonenpc.SpawnPlan, acquiredPlans []zonenpc.SpawnPlan,
) ([]zonenpc.SpawnPlan, map[uint32]struct{}) {
	plans := make([]zonenpc.SpawnPlan, 0, len(spawnPlans)+len(acquiredPlans))
	seenObjectIDs := make(map[uint32]struct{}, len(spawnPlans)+len(acquiredPlans))
	introductionObjectIDs := make(map[uint32]struct{})
	for _, plan := range spawnPlans {
		isImmediateIntroduction :=
			plan.Introduction == zonenpc.SpawnIntroductionFloorWarp ||
				plan.Introduction == zonenpc.SpawnIntroductionAmbush
		if !isImmediateIntroduction {
			continue
		}
		plans = append(plans, plan)
		seenObjectIDs[plan.ObjectID] = struct{}{}
		introductionObjectIDs[plan.ObjectID] = struct{}{}
	}
	for _, plan := range acquiredPlans {
		if _, isFound := seenObjectIDs[plan.ObjectID]; isFound {
			continue
		}
		plans = append(plans, plan)
		seenObjectIDs[plan.ObjectID] = struct{}{}
	}
	return plans, introductionObjectIDs
}
