package raknet103

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonebarrier "github.com/darkspinnet/darkspin/server/zone/barrier"
)

func Create(plans []zonebarrier.Plan) ([][]byte, error) {
	packets, err := create(plans, true)
	if err != nil {
		return nil, fmt.Errorf("closedCreate: %w", err)
	}
	return packets, nil
}

// CreateOpened restores a completed horde's retained doors without collision.
func CreateOpened(plans []zonebarrier.Plan) ([][]byte, error) {
	packets, err := create(plans, false)
	if err != nil {
		return nil, fmt.Errorf("openedCreate: %w", err)
	}
	return packets, nil
}

func create(plans []zonebarrier.Plan, isClosed bool) ([][]byte, error) {
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
				Scale: object.Scale, IsCollidable: isClosed && object.IsCollisionEnabled,
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
		state := "door opened"
		if isClosed {
			state = "door closed"
		}
		statePacket, stateErr := raknet.MarshalApplication(raknet.SetObjectGFXStateMessage{
			ObjectID: object.ObjectID, State: util.HashID(state), Timestamp: 1,
		})
		if stateErr != nil {
			return nil, fmt.Errorf("stateMarshal[%d]: %w", index, stateErr)
		}
		packets = append(packets, statePacket)
	}
	return packets, nil
}

// Open mirrors build-103 OpenDoor (sub_A648D0): clear collision, then publish
// the authored "door opened" graphics state. Deleting the object skips its
// retraction animation entirely; the open placement remains until zone teardown.
func Open(plans []zonebarrier.Plan, timestamp uint64) ([][]byte, error) {
	objectIDs, err := zonebarrier.OpenPublication(plans)
	if err != nil {
		return nil, fmt.Errorf("openPublication: %w", err)
	}
	packets := make([][]byte, 0)
	for index, objectID := range objectIDs {
		messages := []raknet.ApplicationMessage{
			raknet.ObjectCollisionUpdateMessage{ObjectID: objectID},
			raknet.SetObjectGFXStateMessage{
				ObjectID: objectID, State: util.HashID("door opened"), Timestamp: timestamp,
			},
		}
		for messageIndex, message := range messages {
			packet, marshalErr := raknet.MarshalApplication(message)
			if marshalErr != nil {
				return nil, fmt.Errorf("openMarshal[%d][%d]: %w", index, messageIndex, marshalErr)
			}
			packets = append(packets, packet)
		}
	}
	return packets, nil
}
