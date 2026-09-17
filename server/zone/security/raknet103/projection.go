package raknet103

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonesecurity "github.com/darkspinnet/darkspin/server/zone/security"
)

func State(
	objectID uint32, teleport zonesecurity.Teleport,
	isActive bool, isPowerUp bool,
) ([][]byte, error) {
	publication, err := zonesecurity.PublishState(
		objectID, teleport, isActive, isPowerUp,
	)
	if err != nil {
		return nil, fmt.Errorf("statePublication: %w", err)
	}
	return statePublication(publication)
}

func InitialState(objectID [zonesecurity.RouteCount]uint32) ([][]byte, error) {
	publications, err := zonesecurity.PublishInitialState(objectID)
	if err != nil {
		return nil, fmt.Errorf("initialPublication: %w", err)
	}
	packets := make([][]byte, 0, len(publications))
	for index, publication := range publications {
		statePackets, err := statePublication(publication)
		if err != nil {
			return nil, fmt.Errorf("initialMarshal[%d]: %w", index, err)
		}
		packets = append(packets, statePackets...)
	}
	return packets, nil
}

func SnapshotState(snapshot zonesecurity.Snapshot) ([][]byte, error) {
	if snapshot.ObjectID == ([zonesecurity.RouteCount]uint32{}) {
		return nil, nil
	}
	packets := make([][]byte, 0, len(snapshot.ObjectID))
	for index, objectID := range snapshot.ObjectID {
		teleport, isFound := zonesecurity.Route(index)
		if !isFound {
			return nil, fmt.Errorf("snapshotRoute[%d]: unavailable", index)
		}
		statePackets, err := State(
			objectID, teleport, snapshot.Presented[index], false,
		)
		if err != nil {
			return nil, fmt.Errorf("snapshotState[%d]: %w", index, err)
		}
		packets = append(packets, statePackets...)
	}
	return packets, nil
}

type TeleportRequest struct {
	ObjectID    uint32
	Teleport    zonesecurity.Teleport
	Orientation raknet.Quaternion
	Timestamp   uint64
}

func Teleport(req TeleportRequest) ([][]byte, error) {
	publication, err := zonesecurity.PublishTeleport(
		req.ObjectID,
		req.Teleport,
		zonesecurity.Quaternion{
			X: req.Orientation.X, Y: req.Orientation.Y,
			Z: req.Orientation.Z, W: req.Orientation.W,
		},
		req.Timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("teleportPublication: %w", err)
	}
	source := raknet.Vector3{
		X: publication.Source.X, Y: publication.Source.Y,
		Z: publication.Source.Z,
	}
	destination := raknet.Vector3{
		X: publication.Destination.X, Y: publication.Destination.Y,
		Z: publication.Destination.Z,
	}
	messages := []raknet.ApplicationMessage{
		raknet.PositionedEffectMessage{
			Asset:    util.HashID(publication.ActiveEffectName),
			Position: source,
		},
		raknet.PositionedEffectMessage{
			Asset:    util.HashID("character_teleport_beam_out.ServerEventDef"),
			Position: source,
		},
		raknet.SetAnimationStateMessage{
			ObjectID:  publication.ObjectID,
			State:     util.HashID("character_teleport_out"),
			Timestamp: publication.Timestamp, Scale: 1,
		},
		raknet.ObjectTeleportMessage{
			ObjectID: publication.ObjectID, Position: destination,
			Orientation: raknet.Quaternion{
				X: publication.Orientation.X, Y: publication.Orientation.Y,
				Z: publication.Orientation.Z, W: publication.Orientation.W,
			},
		},
		raknet.ObjectUpdateMessage{
			ObjectID:  publication.ObjectID,
			PositionX: destination.X, PositionY: destination.Y,
			PositionZ: destination.Z, IsVisible: true,
		},
		raknet.PositionedEffectMessage{
			Asset:    util.HashID("character_teleport_beam_in_red.ServerEventDef"),
			Position: destination,
		},
		raknet.SetAnimationStateMessage{
			ObjectID:  publication.ObjectID,
			State:     util.HashID("character_teleport_in"),
			Timestamp: publication.Timestamp, Scale: 1,
		},
	}
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("teleportMarshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func statePublication(
	publication zonesecurity.StatePublication,
) ([][]byte, error) {
	position := raknet.Vector3{
		X: publication.Position.X, Y: publication.Position.Y,
		Z: publication.Position.Z,
	}
	packets := make([][]byte, 0, len(publication.EffectNames))
	for index, effectName := range publication.EffectNames {
		packet, err := raknet.MarshalApplication(raknet.ServerEventMessage{
			Asset: util.HashID(effectName), ObjectID: publication.ObjectID,
			Position: position,
		})
		if err != nil {
			return nil, fmt.Errorf("stateMarshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
