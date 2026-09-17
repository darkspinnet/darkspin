package raknet103

import (
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// StealthArrival reconciles hidden travel before the visible attack sequence.
func StealthArrival(plan zonenpc.AttackPlan, timestamp uint64) ([][]byte, error) {
	packets, err := AttackStart(plan, timestamp)
	if err != nil {
		return nil, fmt.Errorf("stealthAttack: %w", err)
	}
	facing := plan.TargetPosition.Sub(plan.SourcePosition)
	yaw := math.Atan2(-float64(facing.X), float64(facing.Y))
	positionPacket, err := raknet.MarshalApplication(raknet.ObjectTeleportMessage{
		ObjectID: plan.SourceObjectID, Position: vector(plan.SourcePosition),
		Orientation: raknet.Quaternion{
			Z: float32(math.Sin(yaw / 2)), W: float32(math.Cos(yaw / 2)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("stealthPosition: %w", err)
	}
	return append([][]byte{positionPacket}, packets...), nil
}
