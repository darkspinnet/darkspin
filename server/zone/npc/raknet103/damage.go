package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	deathraknet "github.com/darkspinnet/darkspin/server/zone/death/raknet103"
)

type DamageRequest struct {
	Target         deathraknet.Target
	SourceObjectID uint32
	HitPoint       float32
	Damage         float32
	SourceTime     uint64
	EffectPool     deathraknet.EffectPool
	IsCritical     bool
	IsDefeated     bool
}

type DamagePublication struct {
	Packet   [][]byte
	DeathRun *deathraknet.Run
}

func Immune(sourceObjectID uint32, targetObjectID uint32) ([]byte, error) {
	if sourceObjectID == 0 || targetObjectID == 0 {
		return nil, errors.New("invalid npc immune publication")
	}
	eventPacket, err := raknet.MarshalApplication(raknet.DamageCombatEventMessage{
		Flags: 0x0040, TargetID: targetObjectID, SourceID: sourceObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("immuneEventMarshal: %w", err)
	}
	return eventPacket, nil
}

func Damage(req DamageRequest) (DamagePublication, error) {
	if req.Target.ObjectID == 0 || req.SourceObjectID == 0 ||
		req.Damage <= 0 || req.EffectPool == nil {
		return DamagePublication{}, errors.New("invalid npc damage publication")
	}
	if req.IsDefeated {
		deathRun, packet, err := deathraknet.NewRun(
			req.Target, req.SourceObjectID, req.Damage, req.IsCritical,
			req.SourceTime, req.EffectPool,
		)
		if err != nil {
			return DamagePublication{}, fmt.Errorf("deathSimulation: %w", err)
		}
		return DamagePublication{Packet: packet, DeathRun: deathRun}, nil
	}
	flags := uint16(0x0001)
	if req.IsCritical {
		flags |= 0x0008
	}
	eventPacket, err := raknet.MarshalApplication(raknet.DamageCombatEventMessage{
		Flags: flags, DeltaHealth: req.Damage,
		TargetID: req.Target.ObjectID, SourceID: req.SourceObjectID,
		IntegerHPChange: -int32(req.Damage),
	})
	if err != nil {
		return DamagePublication{}, fmt.Errorf("eventMarshal: %w", err)
	}
	healthPacket, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
		ObjectID: req.Target.ObjectID, HitPoints: req.HitPoint,
		IsHitPointChanged: true,
	})
	if err != nil {
		return DamagePublication{}, fmt.Errorf("healthMarshal: %w", err)
	}
	return DamagePublication{
		Packet: [][]byte{eventPacket, healthPacket},
	}, nil
}
