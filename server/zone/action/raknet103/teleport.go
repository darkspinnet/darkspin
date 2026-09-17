package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
)

type TeleportRequest struct {
	ObjectID    uint32
	Position    game.Vec3
	Orientation game.Quaternion
}

func Teleport(req TeleportRequest) ([][]byte, error) {
	if req.ObjectID == 0 || !isFinitePosition(req.Position) {
		return nil, errors.New("action teleport invalid")
	}
	teleportPacket, err := raknet.MarshalApplication(raknet.ObjectTeleportMessage{
		ObjectID: req.ObjectID,
		Position: vector(req.Position),
		Orientation: raknet.Quaternion{
			X: req.Orientation.X, Y: req.Orientation.Y,
			Z: req.Orientation.Z, W: req.Orientation.W,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("teleportMarshal: %w", err)
	}
	positionPacket, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID: req.ObjectID, PositionX: req.Position.X,
		PositionY: req.Position.Y, PositionZ: req.Position.Z,
		IsVisible: true,
	})
	if err != nil {
		return nil, fmt.Errorf("positionMarshal: %w", err)
	}
	return [][]byte{teleportPacket, positionPacket}, nil
}

func vector(position game.Vec3) raknet.Vector3 {
	return raknet.Vector3{X: position.X, Y: position.Y, Z: position.Z}
}
