package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
)

// LeapPosition synchronizes the rendered root with authoritative leap travel.
// Ordinary position updates can leave the animation's physics root behind.
func LeapPosition(objectID uint32, position game.Vec3, target game.Vec3) ([]byte, error) {
	if objectID == 0 || !isFiniteVec3(position) || !isFiniteVec3(target) {
		return nil, errors.New("invalid leap position")
	}
	facing := target.Sub(position)
	yaw := math.Atan2(-float64(facing.X), float64(facing.Y))
	packet, err := raknet.MarshalApplication(raknet.ObjectTeleportMessage{
		ObjectID: objectID, Position: vector(position),
		Orientation: raknet.Quaternion{
			Z: float32(math.Sin(yaw / 2)), W: float32(math.Cos(yaw / 2)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("leapPosition: %w", err)
	}
	return packet, nil
}
