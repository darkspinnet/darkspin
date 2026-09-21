package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/squad"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
)

// Create initializes a hero-owned companion before its attributes and effects.
func Create(req raknet.ObjectCreateMessage) ([][]byte, error) {
	position := game.Vec3{X: req.PositionX, Y: req.PositionY, Z: req.PositionZ}
	if req.ObjectID == 0 || req.OwnerID == 0 ||
		req.OwnerID >= zonehero.FirstSharedObjectID() || !isFinitePosition(position) {
		return nil, errors.New("companion create invalid")
	}
	req.PlayerIndex = uint8((req.OwnerID - 1) / squad.Size)
	// The create transform does not initialize reflected position or locomotion.
	// All clients must receive both before any effect can start moving the pet.
	messages := []raknet.ApplicationMessage{
		req,
		raknet.ObjectUpdateMessage{
			ObjectID: req.ObjectID, PositionX: position.X, PositionY: position.Y,
			PositionZ: position.Z, IsVisible: true,
		},
		raknet.ObjectPlayerMoveMessage{
			ObjectID: req.ObjectID, GoalFlags: 0x20, GoalPosition: raknet.Vector3(position),
		},
	}
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("createMarshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
