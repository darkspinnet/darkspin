package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

func Ping(timestamp uint64) ([]byte, error) {
	if timestamp == 0 {
		return nil, errors.New("developer ping invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.TimestampMessage{
		ID: raknet.DebugPing, Timestamp: timestamp,
	})
	if err != nil {
		return nil, fmt.Errorf("pingMarshal: %w", err)
	}
	return packet, nil
}

type DamageRequest struct {
	ObjectID uint32
	Position game.Vec3
	Damage   float32
	HitPoint float32
}

func Damage(req DamageRequest) ([][]byte, error) {
	flags := uint16(0x0001)
	if req.HitPoint == 0 {
		flags |= 0x0004
	}
	packets, err := messages([]raknet.ApplicationMessage{
		raknet.DamageCombatEventMessage{
			Flags: flags, DeltaHealth: req.Damage, TargetID: req.ObjectID,
			SourceID: req.ObjectID, IntegerHPChange: -int32(req.Damage),
		},
		raknet.CombatantDataDeltaMessage{
			ObjectID: req.ObjectID, HitPoints: req.HitPoint,
			IsHitPointChanged: true,
		},
	}, "damage")
	if err != nil {
		return nil, fmt.Errorf("damageMessages: %w", err)
	}
	textPacket, err := effectraknet.CombatText(effectraknet.CombatTextRequest{
		ObjectID: req.ObjectID, Position: req.Position, Amount: req.Damage,
		IsEnemyStyle: true,
	})
	if err != nil {
		return nil, fmt.Errorf("damageText: %w", err)
	}
	return [][]byte{packets[0], textPacket, packets[1]}, nil
}

func Teleport(objectID uint32, destination raknet.Vector3) ([][]byte, error) {
	return messages([]raknet.ApplicationMessage{
		raknet.ObjectTeleportMessage{
			ObjectID: objectID, Position: destination,
			Orientation: raknet.Quaternion{W: 1},
		},
		raknet.ObjectUpdateMessage{
			ObjectID: objectID, PositionX: destination.X,
			PositionY: destination.Y, PositionZ: destination.Z,
			IsVisible: true,
		},
	}, "teleport")
}

func messages(
	messages []raknet.ApplicationMessage,
	operation string,
) ([][]byte, error) {
	packet := make([][]byte, 0, len(messages))
	for index, current := range messages {
		encoded, err := raknet.MarshalApplication(current)
		if err != nil {
			return nil, fmt.Errorf("%sMarshal[%d]: %w", operation, index, err)
		}
		packet = append(packet, encoded)
	}
	return packet, nil
}
