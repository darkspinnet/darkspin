package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
)

const turretLaserAnimation = "TurretLaser"
const turretLaserBeamEffect = "cyber_laser_turret_beam.ServerEventDef"
const turretLaserHitEffect = "cyber_common_hit_small.ServerEventDef"

type TurretLaserRequest struct {
	SourceObjectID uint32
	TargetObjectID uint32
	SourceTime     uint64
}

func TurretLaser(req TurretLaserRequest) ([][]byte, error) {
	if req.SourceObjectID == 0 || req.TargetObjectID == 0 {
		return nil, errors.New("turret laser invalid")
	}
	messages := []raknet.ApplicationMessage{
		raknet.SetAnimationStateMessage{
			ObjectID: req.SourceObjectID, State: util.HashID(turretLaserAnimation),
			Timestamp: req.SourceTime, Scale: 1,
		},
		raknet.ChainedEffectMessage{
			Asset: util.HashID(turretLaserBeamEffect), ObjectID: req.SourceObjectID,
			SecondaryObjectID: req.TargetObjectID,
		},
		raknet.ObjectEffectMessage{
			Asset: util.HashID(turretLaserHitEffect), ObjectID: req.TargetObjectID,
			AttackerID: req.SourceObjectID,
		},
	}
	packets, err := marshalApplicationMessages(messages, "turretLaser")
	if err != nil {
		return nil, fmt.Errorf("turretLaserMarshal: %w", err)
	}
	return packets, nil
}

func TrapSpawn(
	objectID uint32, ownerObjectID uint32, position raknet.Vector3,
	definition sim.AbilityDefinition,
) ([]byte, error) {
	return TrapSpawnForTeam(objectID, ownerObjectID, position, definition, 1)
}

func TrapSpawnForTeam(
	objectID uint32, ownerObjectID uint32, position raknet.Vector3,
	definition sim.AbilityDefinition, team uint8,
) ([]byte, error) {
	if objectID == 0 || ownerObjectID == 0 || definition.SpawnNoun == "" {
		return nil, errors.New("trap spawn invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
		ObjectID: objectID, Noun: util.HashID(definition.SpawnNoun),
		PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
		Scale: 1, Team: team, OwnerID: ownerObjectID,
		IsCollisionEnabled: true,
	})
	if err != nil {
		return nil, fmt.Errorf("trapSpawnMarshal: %w", err)
	}
	return packet, nil
}

func TrapDelete(objectID uint32) ([]byte, error) {
	if objectID == 0 {
		return nil, errors.New("trap delete invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
		ObjectID: []uint32{objectID},
	})
	if err != nil {
		return nil, fmt.Errorf("trapDeleteMarshal: %w", err)
	}
	return packet, nil
}

func TrapObjectEffect(assetName string, objectID uint32, ownerObjectID uint32) ([]byte, error) {
	if assetName == "" || objectID == 0 || ownerObjectID == 0 {
		return nil, errors.New("trap effect invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{
		Asset: util.HashID(assetName), ObjectID: objectID, AttackerID: ownerObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("trapEffectMarshal: %w", err)
	}
	return packet, nil
}

func TrapRemoveEffect(objectID uint32) ([]byte, error) {
	if objectID == 0 {
		return nil, errors.New("trap effect removal invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: 1, IsRemovalRequested: true, IsHardStop: true, ObjectID: objectID,
	})
	if err != nil {
		return nil, fmt.Errorf("trapEffectRemoveMarshal: %w", err)
	}
	return packet, nil
}
