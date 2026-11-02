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

// HopperJump presents NoctHopperJump's JumpInDirection movement to the client.
// The authored jump uses speed 24, heights 2-4, and half its requested jump
// distance as the final physics parameter.
func HopperJump(
	objectID uint32, destination game.Vec3, direction game.Vec3, jumpDistance float32,
) ([]byte, error) {
	if objectID == 0 || !isFiniteVec3(destination) || !isFiniteVec3(direction) ||
		jumpDistance <= 0 || math.IsNaN(float64(jumpDistance)) || math.IsInf(float64(jumpDistance), 0) {
		return nil, errors.New("invalid hopper jump")
	}
	if direction.Length() <= 0 {
		return nil, errors.New("hopper jump direction unavailable")
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectJumpMessage{
		ObjectID: objectID, JumpPosition: vector(destination),
		JumpDirection:  vector(direction),
		JumpParameters: [4]float32{24, 2, 4, jumpDistance * 0.5},
	})
	if err != nil {
		return nil, fmt.Errorf("hopperJumpMarshal: %w", err)
	}
	return packet, nil
}
