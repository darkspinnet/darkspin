package raknet103

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// DrainerRemnants restores only a still-published delayed detonation corpse.
// It cannot restore a living actor or one already marked for deletion.
func DrainerRemnants(snapshots []zonenpc.Snapshot, timestamp uint64) ([][]byte, error) {
	packets := make([][]byte, 0)
	for _, snapshot := range snapshots {
		if !snapshot.IsDefeated || !zonenpc.IsDrainerDeath(snapshot) {
			continue
		}
		if !snapshot.IsPublished {
			deletePacket, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{snapshot.Plan.ObjectID}})
			if err != nil {
				return nil, fmt.Errorf("drainerDelete: %w", err)
			}
			packets = append(packets, deletePacket)
			continue
		}
		spawnPackets, err := spawn(snapshot.FacingSpawnPlan(), true)
		if err != nil {
			return nil, fmt.Errorf("drainerSpawn: %w", err)
		}
		packets = append(packets, spawnPackets...)
		messages := []raknet.ApplicationMessage{
			raknet.CombatantDataDeltaMessage{ObjectID: snapshot.Plan.ObjectID, HitPoints: 0, IsHitPointChanged: true},
			raknet.AgentBlackboardUpdateMessage{ObjectID: snapshot.Plan.ObjectID, IsTargetable: false},
			raknet.ObjectCollisionUpdateMessage{ObjectID: snapshot.Plan.ObjectID, IsCollisionEnabled: false},
		}
		for _, message := range messages {
			packet, err := raknet.MarshalApplication(message)
			if err != nil {
				return nil, fmt.Errorf("drainerState: %w", err)
			}
			packets = append(packets, packet)
		}
		animationPacket, err := AnimationState(snapshot.Plan.ObjectID, "nct_minn_su_drainer_explode", timestamp)
		if err != nil {
			return nil, fmt.Errorf("drainerAnimation: %w", err)
		}
		packets = append(packets, animationPacket)
	}
	return packets, nil
}
