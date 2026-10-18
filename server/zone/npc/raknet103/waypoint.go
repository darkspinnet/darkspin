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
