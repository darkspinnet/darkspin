package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

type EventRequest struct {
	AssetID  uint32
	ObjectID uint32
	Position game.Vec3
}

type ChainHitRequest struct {
	EffectID       uint32
	HitEffectID    uint32
	SourceObjectID uint32
	TargetObjectID uint32
}

type PositionedRequest struct {
	AssetID  uint32
	Position game.Vec3
	Facing   game.Vec3
}

type CombatTextRequest struct {
	ObjectID     uint32
	Position     game.Vec3
	Amount       float32
	IsEnemyStyle bool
	IsCritical   bool
}

func CombatText(req CombatTextRequest) ([]byte, error) {
	if req.ObjectID == 0 || req.Amount <= 0 ||
		math.IsNaN(float64(req.Amount)) || math.IsInf(float64(req.Amount), 0) {
		return nil, errors.New("combat text invalid")
	}
	assetName := "combattext_damage.ServerEventDef"
	if req.IsEnemyStyle {
		assetName = "combattext_enemy_damage.ServerEventDef"
	}
	if req.IsCritical {
		assetName = "combattext_damage_critical.ServerEventDef"
		if req.IsEnemyStyle {
			assetName = "combattext_enemy_damage_critical.ServerEventDef"
		}
	}
	packet, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: util.HashID(assetName), ObjectID: req.ObjectID,
		Position: raknet.Vector3{
			X: req.Position.X, Y: req.Position.Y, Z: req.Position.Z,
		},
		TextValue: uint32(req.Amount),
	})
	if err != nil {
		return nil, fmt.Errorf("combatTextMarshal: %w", err)
	}
	return packet, nil
}

func ChainHit(req ChainHitRequest) ([][]byte, error) {
	if req.EffectID == 0 || req.HitEffectID == 0 ||
		req.SourceObjectID == 0 || req.TargetObjectID == 0 {
		return nil, errors.New("effect chain hit invalid")
	}
	chainPacket, err := raknet.MarshalApplication(raknet.ChainedEffectMessage{
		Asset: req.EffectID, ObjectID: req.SourceObjectID,
		SecondaryObjectID: req.TargetObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("chainMarshal: %w", err)
	}
	hitPacket, err := Event(EventRequest{
		AssetID: req.HitEffectID, ObjectID: req.TargetObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("chainHit: %w", err)
	}
	return [][]byte{chainPacket, hitPacket}, nil
}

func Event(req EventRequest) ([]byte, error) {
	if req.AssetID == 0 {
		return nil, errors.New("effect event invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: req.AssetID, ObjectID: req.ObjectID,
		Position: raknet.Vector3{
			X: req.Position.X, Y: req.Position.Y, Z: req.Position.Z,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("eventMarshal: %w", err)
	}
	return packet, nil
}

func Chain(effectID uint32, sourceObjectID uint32, targetObjectID uint32) ([]byte, error) {
	if effectID == 0 || sourceObjectID == 0 || targetObjectID == 0 {
		return nil, errors.New("effect chain invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.ChainedEffectMessage{
		Asset: effectID, ObjectID: sourceObjectID,
		SecondaryObjectID: targetObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("chainMarshal: %w", err)
	}
	return packet, nil
}

func Positioned(req PositionedRequest) ([]byte, error) {
	if req.AssetID == 0 {
		return nil, errors.New("positioned effect invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.PositionedEffectMessage{
		Asset: req.AssetID,
		Position: raknet.Vector3{
			X: req.Position.X, Y: req.Position.Y, Z: req.Position.Z,
		},
		Facing: raknet.Vector3{
			X: req.Facing.X, Y: req.Facing.Y, Z: req.Facing.Z,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("positionedMarshal: %w", err)
	}
	return packet, nil
}
