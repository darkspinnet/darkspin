package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/raknet"
)

type HitRequest struct {
	SourceObjectID uint32
	TargetObjectID uint32
	Damage         float32
	HitPoint       float32
	IsCritical     bool
}

func Hit(req HitRequest) ([][]byte, error) {
	if req.SourceObjectID == 0 || req.TargetObjectID == 0 ||
		!isFiniteScalar(req.Damage) || req.Damage <= 0 ||
		!isFiniteScalar(req.HitPoint) || req.HitPoint < 0 {
		return nil, errors.New("npc hit invalid")
	}
	flags := uint16(0x0001)
	if req.HitPoint == 0 {
		flags |= 0x0004
	}
	if req.IsCritical {
		flags |= 0x0008
	}
	eventPacket, err := raknet.MarshalApplication(raknet.DamageCombatEventMessage{
		Flags: flags, DeltaHealth: req.Damage,
		TargetID: req.TargetObjectID, SourceID: req.SourceObjectID,
		IntegerHPChange: -int32(req.Damage),
	})
	if err != nil {
		return nil, fmt.Errorf("hitEventMarshal: %w", err)
	}
	healthPacket, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
		ObjectID: req.TargetObjectID, HitPoints: req.HitPoint,
		IsHitPointChanged: true,
	})
	if err != nil {
		return nil, fmt.Errorf("hitHealthMarshal: %w", err)
	}
	return [][]byte{eventPacket, healthPacket}, nil
}

func isFiniteScalar(number float32) bool {
	return !math.IsNaN(float64(number)) && !math.IsInf(float64(number), 0)
}
