package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
)

// marshalPlayerMovementCorrection reconciles rejected local prediction. Native
// 103's fixed move receiver (5393D0) skips the controlling hero, so sending a
// stop or a return goal through that receiver cannot correct the owner's pose.
// Reflection (539900) applies the stop to the owner; teleport (5392F0) resets
// physics and places the object, including the controlled hero's input pose.
func marshalPlayerMovementCorrection(
	objectID uint32, position raknet.Vector3, orientation raknet.Quaternion,
) ([][]byte, error) {
	if objectID == 0 || !isFiniteZonePosition(position) {
		return nil, errors.New("invalid movement correction")
	}
	if orientation.X == 0 && orientation.Y == 0 &&
		orientation.Z == 0 && orientation.W == 0 {
		orientation.W = 1
	}
	goalFlags := uint32(0x20)
	targetObjectID := uint32(0)
	externalMotion := raknet.Vector3{}
	stopDistance := float32(0)
	stopPacket, err := raknet.MarshalApplication(raknet.LocomotionUpdateContractMessage{
		ObjectID: objectID,
		Locomotion: raknet.LocomotionReflection{
			GoalFlags: &goalFlags, GoalPosition: &position,
			PartialGoalPosition: &position, TargetObjectID: &targetObjectID,
			ExternalLinearVelocity: &externalMotion, ExternalForce: &externalMotion,
			AllowedStopDistance: &stopDistance, DesiredStopDistance: &stopDistance,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("correctionStop: %w", err)
	}
	placementPacket, err := raknet.MarshalApplication(raknet.ObjectTeleportMessage{
		ObjectID: objectID, Position: position, Orientation: orientation,
	})
	if err != nil {
		return nil, fmt.Errorf("correctionPlacement: %w", err)
	}
	return [][]byte{stopPacket, placementPacket}, nil
}
