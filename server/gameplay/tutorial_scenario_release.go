//go:build !scenario

package gameplay

import zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"

func attachScenarioTutorialAction(requestID string, plan zonenpc.SpawnPlan, programs Programs) (zonenpc.SpawnPlan, error) {
	return plan, nil
}
