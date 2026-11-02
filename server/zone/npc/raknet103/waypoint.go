package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
)

// Native 5393D0 stores the ultimate goal while remote movement consumes the
// partial goal at component+340. Use a fixed-point command for the next server
// corridor point; the server owns attack-range stopping and target selection.
func PursuitWaypoint(objectID uint32, waypoint game.Vec3) ([][]byte, error) {
	if objectID == 0 || !isFiniteVec3(waypoint) {
		return nil, errors.New("npc pursuit waypoint invalid")
	}
	packets, err := marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectPlayerMoveMessage{
			ObjectID: objectID, GoalFlags: 0x01, GoalPosition: vector(waypoint),
		},
	}, "pursuitWaypoint")
	if err != nil {
		return nil, fmt.Errorf("waypointMarshal: %w", err)
	}
	return packets, nil
}

// CircleTarget composes MoveToPointExact (103 A147D0, flag 1) with
// FaceObjectDuringMove (103 A00740, flag 0x100 and the facing target ID).
// Object-follow flag 0x40 would steer toward the hero instead of the circle.
func CircleTargetMovement(objectID uint32, destination game.Vec3, targetObjectID uint32) ([][]byte, error) {
	if objectID == 0 || targetObjectID == 0 || !isFiniteVec3(destination) {
		return nil, errors.New("npc circle movement invalid")
	}
	packets, err := marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectPlayerMoveMessage{
			ObjectID: objectID, GoalFlags: 0x101, GoalPosition: vector(destination),
			TargetObjectID: targetObjectID,
		},
		raknet.LocomotionUnreliableMessage{
			ObjectID: objectID, GoalPosition: vector(destination),
		},
	}, "circleMovement")
	if err != nil {
		return nil, fmt.Errorf("circleMarshal: %w", err)
	}
	return packets, nil
}
