package raknet103

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
)

// FollowModifier requests MoveToPointExact, not pursuit of a hostile target.
func OrcusFollow(objectID uint32, position, destination game.Vec3) ([][]byte, error) {
	packets, err := marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectPositionUpdateMessage{ObjectID: objectID,
			PositionX: position.X, PositionY: position.Y, PositionZ: position.Z},
		raknet.ObjectPlayerMoveMessage{ObjectID: objectID, GoalFlags: 0x01,
			GoalPosition: vector(destination), Facing: direction(vector(position), vector(destination))},
		raknet.LocomotionUnreliableMessage{ObjectID: objectID, GoalPosition: vector(destination)},
	}, "orcusFollow")
	if err != nil {
		return nil, fmt.Errorf("followMarshal: %w", err)
	}
	return packets, nil
}
