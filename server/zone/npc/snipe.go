package npc

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

func ShouldRefreshSnipeSlow(timestamp uint64, slowEndTimestamp uint64) bool {
	return slowEndTimestamp != 0 && timestamp >= slowEndTimestamp
}

func PlanSnipeSlow(
	npc Snapshot, targetObjectID uint32, targetPosition game.Vec3,
	targetFootprintRadius float32,
) (AttackPlan, error) {
	profile, isFound := ActionProfileForNoun(npc.Plan.NounName)
	if !isFound || profile.Family != ActionNomadSnipe ||
		profile.ModifierName == "" || profile.ModifierDuration <= 0 {
		return AttackPlan{}, errors.New("npc slow unsupported")
	}
	if npc.IsDefeated || npc.HitPoint <= 0 || targetObjectID == 0 ||
		npc.TargetObjectID != targetObjectID {
		return AttackPlan{}, errors.New("npc slow target unavailable")
	}
	stopDistance, err := zoneaction.NPCStopDistance(
		profile.Range, npc.Plan.NPCProfile.FootprintRadius,
		targetFootprintRadius,
	)
	if err != nil {
		return AttackPlan{}, fmt.Errorf("npcSlowRange: %w", err)
	}
	profile.Range = stopDistance
	if zonegeometry.Distance(npc.Plan.Position, targetPosition) >= profile.Range {
		return AttackPlan{}, errors.New("npc slow target out of range")
	}
	return AttackPlan{
		SourceObjectID:   npc.Plan.ObjectID,
		TargetObjectID:   targetObjectID,
		ActionGeneration: npc.ActionGeneration,
		SourcePosition:   npc.Plan.Position,
		TargetPosition:   targetPosition,
		Profile:          profile,
	}, nil
}
