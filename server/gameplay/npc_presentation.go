package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// The caller returns these exact initial bytes through a guarded producer that
// publishes them to peers after commit. Do not also enqueue transient aggro
// events; observers may drain those before that producer finishes writing.
func (e campaignNPCActionRuntime) scheduleOwnedFirstActions(
	packet raknet.Packet, sessionKey string, generation uint64,
	plans []zonenpc.SpawnPlan, timestamp uint64, startedAt time.Time,
) ([][]byte, error) {
	packets, err := e.scheduleFirstActionsWithIntroductions(packet, sessionKey,
		generation, plans, timestamp, nil, npcInitialPublication{startedAt: startedAt})
	if err != nil {
		return packets, fmt.Errorf("ownedFirstActions: %w", err)
	}
	return packets, nil
}

type npcInitialPublication struct {
	startedAt time.Time
}

type npcInitialPacketStep struct {
	packets [][]byte
}

func (e npcInitialPacketStep) produce() ([][]byte, error) {
	return e.packets, nil
}
