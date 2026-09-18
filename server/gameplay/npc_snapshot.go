package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func marshalCampaignNPCSnapshot(snapshot zonenpc.Snapshot) ([][]byte, error) {
	packets, err := npcraknet.Spawn(snapshot.FacingSpawnPlan())
	if err != nil {
		return nil, fmt.Errorf("snapshotSpawn: %w", err)
	}
	resourcePacket, err := raknet.MarshalApplication(raknet.CombatantDataUpdateMessage{
		ObjectID: snapshot.Plan.ObjectID, HitPoints: snapshot.HitPoint,
		ManaPoints: snapshot.ManaPoint,
	})
	if err != nil {
		return nil, fmt.Errorf("snapshotResource: %w", err)
	}
	packets = append(packets, resourcePacket)
	if snapshot.TargetObjectID == 0 {
		stopPackets, stopErr := npcraknet.MovementStop(snapshot.Plan.ObjectID, snapshot.Plan.Position)
		if stopErr != nil {
			return nil, fmt.Errorf("snapshotStop: %w", stopErr)
		}
		packets = append(packets, stopPackets...)
	}
	targetPackets, err := npcraknet.TargetUpdates([]zonenpc.Snapshot{snapshot})
	if err != nil {
		return nil, fmt.Errorf("snapshotTarget: %w", err)
	}
	return append(packets, targetPackets...), nil
}
