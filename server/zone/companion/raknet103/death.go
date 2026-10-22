package raknet103

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
)

func Death(actor zonecompanion.Actor, profile zonecompanion.DeathProfile, timestamp uint64) ([][]byte, error) {
	messages := []raknet.ApplicationMessage{
		raknet.AgentBlackboardUpdateMessage{ObjectID: actor.ObjectID, IsTargetable: false},
		raknet.ObjectPlayerMoveMessage{ObjectID: actor.ObjectID, GoalFlags: 0x20, GoalPosition: raknet.Vector3(actor.Position)},
		raknet.ObjectCollisionUpdateMessage{ObjectID: actor.ObjectID, IsCollisionEnabled: false},
		raknet.SetAnimationStateMessage{ObjectID: actor.ObjectID, State: util.HashID(profile.AnimationName), Timestamp: timestamp, Scale: 1},
		raknet.CombatantDataDeltaMessage{ObjectID: actor.ObjectID, HitPoints: 0, IsHitPointChanged: true},
	}
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("petDeath[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
