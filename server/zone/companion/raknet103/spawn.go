package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/util"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
)

type SpawnRequest struct {
	ObjectID      uint32
	OwnerObjectID uint32
	Intent        sim.CompanionSpawnIntent
	Position      game.Vec3
}

func Spawn(req SpawnRequest) ([][]byte, error) {
	if req.ObjectID == 0 || req.OwnerObjectID == 0 || req.OwnerObjectID >= zonehero.FirstSharedObjectID() ||
		req.Intent.NounName == "" || req.Intent.SpawnEffectID == 0 ||
		req.Intent.SpawnAbilityID == 0 || req.Intent.BurrowModifierID == 0 ||
		!isFinitePosition(req.Position) {
		return nil, errors.New("companion spawn invalid")
	}
	position := raknet.Vector3{
		X: req.Position.X, Y: req.Position.Y, Z: req.Position.Z,
	}
	createPacket, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
		ObjectID: req.ObjectID, Noun: util.HashID(req.Intent.NounName),
		PositionX: req.Position.X, PositionY: req.Position.Y,
		PositionZ: req.Position.Z, Scale: 1, Team: 1,
		OwnerID: req.OwnerObjectID, IsCollisionEnabled: true,
		PlayerIndex: uint8((req.OwnerObjectID - 1) / squad.Size),
	})
	if err != nil {
		return nil, fmt.Errorf("spawnCreate: %w", err)
	}
	// The create transform alone does not initialize reflected object position.
	// Publish it and an explicit idle goal before effects or subsequent movement.
	positionPacket, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID: req.ObjectID, PositionX: position.X, PositionY: position.Y,
		PositionZ: position.Z, IsVisible: true,
	})
	if err != nil {
		return nil, fmt.Errorf("spawnPosition: %w", err)
	}
	stopPacket, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
		ObjectID: req.ObjectID, GoalFlags: 0x20, GoalPosition: position,
	})
	if err != nil {
		return nil, fmt.Errorf("spawnStop: %w", err)
	}
	attributePacket, err := raknet.MarshalApplication(
		raknet.AttributeDataUpdateMessage{
			ObjectID: req.ObjectID,
			Value: map[uint8]float32{
				11: zonecompanion.CompatibilityMovementSpeed,
				12: zonecompanion.CompatibilityMovementSpeed,
				48: 0,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("spawnAttribute: %w", err)
	}
	effectPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: req.Intent.SpawnEffectID, ObjectID: req.ObjectID,
		Position: position,
	})
	if err != nil {
		return nil, fmt.Errorf("spawnEffect: %w", err)
	}
	return [][]byte{createPacket, positionPacket, stopPacket, attributePacket, effectPacket}, nil
}

func isFinitePosition(position game.Vec3) bool {
	return !math.IsNaN(float64(position.X)) &&
		!math.IsInf(float64(position.X), 0) &&
		!math.IsNaN(float64(position.Y)) &&
		!math.IsInf(float64(position.Y), 0) &&
		!math.IsNaN(float64(position.Z)) &&
		!math.IsInf(float64(position.Z), 0)
}
