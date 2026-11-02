package npc

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

// PlanFastSwipeHit follows build 103 chunk 934's stillInRange=2.5 check for
// each of its nine hits, measured beyond the two actor footprints.
func PlanFastSwipeHit(
	source Snapshot, targetObjectID uint32, targetPosition game.Vec3,
	profile ActionProfile, targetFootprintRadius float32,
) (AttackPlan, error) {
	hitDistance := source.Plan.ActorFootprintRadius() + targetFootprintRadius + profile.Range
	if zonegeometry.Distance(source.Plan.Position, targetPosition) > hitDistance {
		return AttackPlan{}, ErrControlTargetOutOfRange
	}
	plan, err := PlanAreaAttackWithProfile(source, targetObjectID, targetPosition, profile)
	if err != nil {
		return AttackPlan{}, fmt.Errorf("swipeHit: %w", err)
	}
	return plan, nil
}
