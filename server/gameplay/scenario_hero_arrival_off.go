//go:build !scenario

package gameplay

import "github.com/darkspinnet/darkspin/server/raknet"

func (e gameplayPendingRuntime) observeScenarioHeroClock(packet raknet.Packet) {}

func (e gameplayPendingRuntime) pollScenarioHeroArrival(packet raknet.Packet) ([][]byte, error) {
	return nil, nil
}
