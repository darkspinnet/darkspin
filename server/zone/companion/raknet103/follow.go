package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
)

func Follow(plans []zonecompanion.Follow) ([][]byte, error) {
	packets := make([][]byte, 0, len(plans)*2)
	for index, current := range plans {
		if current.ObjectID == 0 || current.OwnerObjectID == 0 ||
			current.DesiredStopDistance <= 0 {
			return nil, fmt.Errorf("follow[%d]: invalid", index)
		}
		// TravelDuration and Revision belong to scheduled server arrival. The
		// Healing Sprite commits its position on its tick and has no arrival job.
		facingX := current.Goal.X - current.Position.X
		facingY := current.Goal.Y - current.Position.Y
		facingZ := current.Goal.Z - current.Position.Z
		facingLength := float32(math.Sqrt(float64(
			facingX*facingX + facingY*facingY + facingZ*facingZ,
		)))
		if facingLength <= 0 {
			return nil, fmt.Errorf("follow[%d]: zero direction", index)
		}
		movePacket, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
			ObjectID: current.ObjectID, GoalFlags: 0x41,
			GoalPosition: raknet.Vector3{
				X: current.Goal.X, Y: current.Goal.Y, Z: current.Goal.Z,
			},
			Facing: raknet.Vector3{
				X: facingX / facingLength,
				Y: facingY / facingLength,
				Z: facingZ / facingLength,
			},
			AllowedStopDistance: current.DesiredStopDistance,
			DesiredStopDistance: current.DesiredStopDistance,
			TargetPosition: raknet.Vector3{
				X: current.Goal.X, Y: current.Goal.Y, Z: current.Goal.Z,
			},
			TargetObjectID: current.OwnerObjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("followMarshal[%d]: %w", index, err)
		}
		if len(movePacket) == 0 {
			return nil, errors.New("follow packet empty")
		}
		goal := raknet.Vector3{
			X: current.Goal.X, Y: current.Goal.Y, Z: current.Goal.Z,
		}
		correctionPacket, err := raknet.MarshalApplication(
			raknet.LocomotionUnreliableMessage{
				ObjectID: current.ObjectID, GoalPosition: goal,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("followCorrectionMarshal[%d]: %w", index, err)
		}
		packets = append(packets, movePacket, correctionPacket)
	}
	return packets, nil
}
