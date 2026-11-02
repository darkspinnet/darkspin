//go:build !scenario

package gameplay

import (
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e *gameplaySessionRegistry) prepareScenarioOwnedPlanLocked(requestID string, member gameplayPeerSession, plan zonenpc.SpawnPlan) (zonenpc.SpawnPlan, bool, error) {
	return plan, false, nil
}

func (e gameplayPendingRuntime) finishScenarioOwnedSpawn(packet raknet.Packet, requestID string) error {
	return nil
}
