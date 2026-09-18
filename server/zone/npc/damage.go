package npc

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

// DamageMetadata preserves independent damage categories through every hit path.
type DamageMetadata struct {
	DamageSource      uint32
	DamageType        uint32
	DescriptorMask    uint32
	IsDamageTypeKnown bool
	isForcedDefeat    bool
}

type HitRequest struct {
	SourceObjectID uint32
	TargetObjectID uint32
	Damage         float32
	SourcePosition *game.Vec3
	Metadata       DamageMetadata
	IsArea         bool
	IsPeriodic     bool
}

func (e *Session) Hit(req HitRequest) (DamageResult, error) {
	if req.SourcePosition != nil && !zonegeometry.IsFinite(*req.SourcePosition) {
		return DamageResult{}, fmt.Errorf("hitPosition: nonfinite")
	}
	result, err := e.damage(req.SourceObjectID, req.TargetObjectID, req.Damage,
		req.SourcePosition, req.IsArea, req.IsPeriodic, []uint32{req.Metadata.DamageSource}, req.Metadata)
	if err != nil {
		return DamageResult{}, fmt.Errorf("hitApply: %w", err)
	}
	return result, nil
}

// Defeat is an encounter lifecycle operation, not an attack. It preserves the
// shared death bookkeeping while bypassing defense, shields and phase thresholds.
func (e *Session) Defeat(sourceObjectID, targetObjectID uint32) (DamageResult, error) {
	result, err := e.damage(sourceObjectID, targetObjectID, 1, nil, false, false,
		nil, DamageMetadata{isForcedDefeat: true})
	if err != nil {
		return DamageResult{}, fmt.Errorf("defeatApply: %w", err)
	}
	return result, nil
}
