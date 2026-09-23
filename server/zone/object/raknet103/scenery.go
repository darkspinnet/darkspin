package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
)

func DeleteScenery(objectIDs []uint32) ([]byte, error) {
	if len(objectIDs) == 0 {
		return nil, nil
	}
	for _, objectID := range objectIDs {
		if objectID == 0 {
			return nil, errors.New("scenery delete object invalid")
		}
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: objectIDs})
	if err != nil {
		return nil, fmt.Errorf("sceneryDeleteMarshal: %w", err)
	}
	return packet, nil
}

func Scenery(plan zoneobject.SceneryPlan) ([][]byte, error) {
	publication, err := zoneobject.PublishScenery(plan)
	if err != nil {
		return nil, fmt.Errorf("sceneryPublication: %w", err)
	}
	createPacket, err := raknet.MarshalApplication(raknet.EnemyObjectCreateMessage{
		ObjectID: publication.ObjectID, Noun: util.HashID(publication.NounName),
		Position: raknet.Vector3{
			X: publication.Position.X, Y: publication.Position.Y, Z: publication.Position.Z,
		},
		Rotation: raknet.Vector3{
			X: publication.Rotation.X, Y: publication.Rotation.Y, Z: publication.Rotation.Z,
		},
		Scale: publication.Scale, IsCollidable: publication.IsCollisionEnabled,
	})
	if err != nil {
		return nil, fmt.Errorf("sceneryCreateMarshal: %w", err)
	}
	updatePacket, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID: publication.ObjectID, PositionX: publication.Position.X,
		PositionY: publication.Position.Y, PositionZ: publication.Position.Z,
		IsVisible: publication.IsVisible,
	})
	if err != nil {
		return nil, fmt.Errorf("sceneryUpdateMarshal: %w", err)
	}
	return [][]byte{createPacket, updatePacket}, nil
}
