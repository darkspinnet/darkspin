package raknet103

import (
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Remnants restores permanent wrecks without replaying their destruction or
// making them attackable again. Ordinary enemy corpses are not retained here.
func Remnants(snapshots []zonenpc.Snapshot) ([][]byte, error) {
	packets := make([][]byte, 0)
	for _, snapshot := range snapshots {
		isGeyser := zonenpc.IsCryosGeyser(snapshot.Plan)
		isBossVent := strings.EqualFold(snapshot.Plan.NounName, "DEST_citadel_factoryvent_boss.Noun")
		isPipe := zonenpc.IsFactoryPipe(snapshot.Plan.NounName)
		isWarpTotem := zonenpc.IsWarpTotem(snapshot.Plan)
		if !snapshot.IsPublished || !snapshot.IsDefeated ||
			(!zonenpc.IsGraviticRemnant(snapshot.Plan) && !isGeyser && !isBossVent && !isPipe && !isWarpTotem) {
			continue
		}
		graphicsState := util.HashID("dead")
		if isGeyser {
			graphicsState = util.HashID("unblocked")
		}
		spawnPackets, err := spawn(snapshot.FacingSpawnPlan(), true)
		if err != nil {
			return nil, fmt.Errorf("remnantSpawn[%d]: %w", snapshot.Plan.ObjectID, err)
		}
		packets = append(packets, spawnPackets...)
		messages := []raknet.ApplicationMessage{
			raknet.CombatantDataDeltaMessage{
				ObjectID: snapshot.Plan.ObjectID, HitPoints: 0, IsHitPointChanged: true,
			},
			raknet.AgentBlackboardUpdateMessage{
				ObjectID: snapshot.Plan.ObjectID, IsTargetable: false,
			},
			raknet.SetObjectGFXStateMessage{
				ObjectID: snapshot.Plan.ObjectID, State: graphicsState,
			},
			raknet.ObjectCollisionUpdateMessage{
				ObjectID: snapshot.Plan.ObjectID, IsCollisionEnabled: !isGeyser && !isPipe && !isWarpTotem,
			},
		}
		for index, message := range messages {
			packet, err := raknet.MarshalApplication(message)
			if err != nil {
				return nil, fmt.Errorf("remnantState[%d]: %w", index, err)
			}
			packets = append(packets, packet)
		}
	}
	return packets, nil
}
