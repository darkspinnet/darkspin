package raknet103

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonebarrier "github.com/darkspinnet/darkspin/server/zone/barrier"
)

func Create(plans []zonebarrier.Plan) ([][]byte, error) {
	publication, err := zonebarrier.CreatePublication(plans)
	if err != nil {
		return nil, fmt.Errorf("createPublication: %w", err)
	}
	packets := make([][]byte, 0, len(publication)*2)
	for index, object := range publication {
		createPacket, err := raknet.MarshalApplication(
			raknet.EnemyObjectCreateMessage{
				ObjectID: object.ObjectID, Noun: util.HashID(object.NounName),
				Position: raknet.Vector3{
					X: object.Position.X, Y: object.Position.Y,
					Z: object.Position.Z,
				},
				Rotation: raknet.Vector3{
					X: object.Rotation.X, Y: object.Rotation.Y,
					Z: object.Rotation.Z,
				},
				Scale: object.Scale, IsCollidable: object.IsCollisionEnabled,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("createMarshal[%d]: %w", index, err)
		}
		updatePacket, err := raknet.MarshalApplication(
			raknet.ObjectUpdateMessage{
				ObjectID:  object.ObjectID,
				PositionX: object.Position.X, PositionY: object.Position.Y,
				PositionZ: object.Position.Z, IsVisible: object.IsVisible,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("updateMarshal[%d]: %w", index, err)
		}
		packets = append(packets, createPacket, updatePacket)
	}
	return packets, nil
}

func Delete(plans []zonebarrier.Plan) ([]byte, error) {
	objectID, err := zonebarrier.DeletePublication(plans)
	if err != nil {
		return nil, fmt.Errorf("deletePublication: %w", err)
	}
	packet, err := raknet.MarshalApplication(
		raknet.ObjectDeleteMessage{ObjectID: objectID},
	)
	if err != nil {
		return nil, fmt.Errorf("deleteMarshal: %w", err)
	}
	return packet, nil
}
