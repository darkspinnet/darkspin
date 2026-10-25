//go:build !scenario

package gameplay

import (
	"context"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type scenarioSpawnState struct{}

func (e *gameplaySessionRegistry) prepareScenarioSpawnPlanLocked(requestID string, plan zonenpc.SpawnPlan) (zonenpc.SpawnPlan, error) {
	return plan, nil
}

func (e *gameplaySessionRegistry) validateScenarioSpawnLocked(requestID string, peerSession gameplayPeerSession) error {
	return nil
}
func (e gameplayPendingRuntime) consumeScenarioSpawn(ctx context.Context, packet raknet.Packet, peerSession gameplayPeerSession, isEligible bool) ([][]byte, bool, error) {
	return nil, false, nil
}
func (e *gameplaySessionRegistry) recordScenarioSpawnAdmissionLocked(requestID string, peerSession gameplayPeerSession, plan zonenpc.SpawnPlan) {
}
func (e *gameplaySessionRegistry) recordScenarioSpawnFailure(requestID string, err error) {}
func (e *gameplaySessionRegistry) finishScenarioSpawnPublication(packet raknet.Packet, requestID string) error {
	return nil
}
