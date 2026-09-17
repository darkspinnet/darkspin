package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
)

func GateRepel(objectID uint32, contact zonehorde.GateContact) ([][]byte, error) {
	if objectID == 0 || contact.MarkerID == 0 || contact.MarkerSetName == "" ||
		!isFinitePosition(contact.ReturnPosition.X, contact.ReturnPosition.Y, contact.ReturnPosition.Z) {
		return nil, errors.New("gate repel invalid")
	}
	position := raknet.Vector3{
		X: contact.ReturnPosition.X,
		Y: contact.ReturnPosition.Y,
		Z: contact.ReturnPosition.Z,
	}
	stopPacket, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
		ObjectID: objectID, GoalFlags: 0x20, GoalPosition: position,
	})
	if err != nil {
		return nil, fmt.Errorf("gateStopMarshal: %w", err)
	}
	teleportPacket, err := raknet.MarshalApplication(
		raknet.ObjectTeleportMessage{ObjectID: objectID, Position: position},
	)
	if err != nil {
		return nil, fmt.Errorf("gateTeleportMarshal: %w", err)
	}
	return [][]byte{stopPacket, teleportPacket}, nil
}

func isFinitePosition(coordinates ...float32) bool {
	for _, current := range coordinates {
		if math.IsNaN(float64(current)) || math.IsInf(float64(current), 0) {
			return false
		}
	}
	return true
}
