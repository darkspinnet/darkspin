package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone/npc"
)

func GravityOrbSpawn(
	objectID uint32, ownerObjectID uint32, position game.Vec3,
	profile npc.GravityOrbProfile,
) ([][]byte, error) {
	if objectID == 0 || ownerObjectID == 0 || profile.NounName == "" ||
		profile.StartupEffectName == "" {
		return nil, errors.New("gravity orb spawn invalid")
	}
	createPacket, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
		ObjectID: objectID, Noun: util.HashID(profile.NounName),
		PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
		Scale: 1, Team: 0, OwnerID: ownerObjectID, IsCollisionEnabled: true,
	})
	if err != nil {
		return nil, fmt.Errorf("gravityOrbCreate: %w", err)
	}
	effectPacket, err := GravityOrbAttachedEffect(
		profile.StartupEffectName, objectID,
	)
	if err != nil {
		return nil, fmt.Errorf("gravityOrbStartup: %w", err)
	}
	return [][]byte{createPacket, effectPacket}, nil
}

func GravityOrbAttachedEffect(
	assetName string, objectID uint32,
) ([]byte, error) {
	if assetName == "" || objectID == 0 {
		return nil, errors.New("gravity orb attached effect invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: 1, Asset: util.HashID(assetName), ObjectID: objectID,
		IsForceAttached: true,
	})
	if err != nil {
		return nil, fmt.Errorf("gravityOrbAttachedEffect: %w", err)
	}
	return packet, nil
}

func GravityOrbEffectRemoval(objectID uint32) ([]byte, error) {
	if objectID == 0 {
		return nil, errors.New("gravity orb effect removal invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: 1, ObjectID: objectID,
		IsRemovalRequested: true, IsHardStop: true,
	})
	if err != nil {
		return nil, fmt.Errorf("gravityOrbEffectRemoval: %w", err)
	}
	return packet, nil
}

func GravityOrbEffect(
	assetName string, position game.Vec3,
) ([]byte, error) {
	if assetName == "" || !isFiniteVec3(position) {
		return nil, errors.New("gravity orb effect invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.DropPresentationMessage{
		Asset: util.HashID(assetName), Position: vector(position),
	})
	if err != nil {
		return nil, fmt.Errorf("gravityOrbEffect: %w", err)
	}
	return packet, nil
}

func GravityOrbDelete(objectID uint32) ([]byte, error) {
	if objectID == 0 {
		return nil, errors.New("gravity orb delete invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
		ObjectID: []uint32{objectID},
	})
	if err != nil {
		return nil, fmt.Errorf("gravityOrbDelete: %w", err)
	}
	return packet, nil
}
