package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// LaserZoneStart anchors both ends of each beam to its placement positions.
// Cast-owned markers also keep cleanup from stopping a newer cast's effects.
func LaserZoneStart(plan zonenpc.AttackPlan, objectIDs []uint32, endpoints []game.Vec3) ([][]byte, error) {
	if len(endpoints) == 0 || len(objectIDs) != len(endpoints)+1 ||
		len(endpoints) > 4 || plan.Profile.TrailEffectName == "" {
		return nil, errors.New("laser zone placement invalid")
	}
	positions := append([]game.Vec3{plan.SourcePosition}, endpoints...)
	messages := make([]raknet.ApplicationMessage, 0, len(positions)+len(endpoints))
	for index, position := range positions {
		if objectIDs[index] == 0 || !isFiniteVec3(position) {
			return nil, errors.New("laser zone marker invalid")
		}
		messages = append(messages, raknet.ObjectCreateMessage{
			ObjectID: objectIDs[index], Noun: util.HashID(twinLaserEndpointNounName),
			PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
			Scale: 1, OwnerID: plan.SourceObjectID, IsCollisionEnabled: false,
		})
	}
	for index := range endpoints {
		messages = append(messages, raknet.AttachedEffectMessage{
			Slot: uint8(index + 1), IsForceAttached: true,
			Asset:    util.HashID(plan.Profile.TrailEffectName),
			ObjectID: objectIDs[0], SecondaryObjectID: objectIDs[index+1],
		})
	}
	packets, err := marshalMessages(messages, "laserZoneStart")
	if err != nil {
		return nil, fmt.Errorf("laserStart: %w", err)
	}
	return packets, nil
}

func LaserZoneEnd(objectIDs []uint32) ([][]byte, error) {
	if len(objectIDs) < 2 {
		return nil, errors.New("laser zone cleanup invalid")
	}
	messages := make([]raknet.ApplicationMessage, 0, len(objectIDs))
	for index := 1; index < len(objectIDs); index++ {
		messages = append(messages, raknet.AttachedEffectMessage{
			Slot: uint8(index), ObjectID: objectIDs[0],
			IsRemovalRequested: true, IsHardStop: true,
		})
	}
	messages = append(messages, raknet.ObjectDeleteMessage{ObjectID: objectIDs})
	packets, err := marshalMessages(messages, "laserZoneEnd")
	if err != nil {
		return nil, fmt.Errorf("laserEnd: %w", err)
	}
	return packets, nil
}
