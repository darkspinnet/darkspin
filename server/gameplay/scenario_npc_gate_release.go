//go:build !scenario

package gameplay

import zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"

func scenarioAutonomousNPCPlans(session *zonenpc.Session, plans []zonenpc.SpawnPlan) []zonenpc.SpawnPlan {
	return plans
}

func (e campaignNPCActionRuntime) finishScenarioNPCManualAction(sessionKey string, generation uint64, objectID uint32, actionGeneration uint64) bool {
	return false
}
