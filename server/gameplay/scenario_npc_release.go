//go:build !scenario

package gameplay

import (
	"context"
	"github.com/darkspinnet/darkspin/server/raknet"
)

type scenarioNPCControlQueue struct{}

func (e gameplayPendingRuntime) consumeScenarioNPCControl(ctx context.Context, packet raknet.Packet, peer gameplayPeerSession, isEligible bool) ([][]byte, bool, error) {
	return nil, false, nil
}
