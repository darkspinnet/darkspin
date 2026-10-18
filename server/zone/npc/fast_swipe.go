package npc

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

// PlanFastSwipeHit follows FastSwipe's stillInRange=0 check for each of its
// nine hits. Its 2.5-unit ability range admits the cast, not the damage.
func PlanFastSwipeHit(
	source Snapshot, targetObjectID uint32, targetPosition game.Vec3,
	profile ActionProfile, targetFootprintRadius float32,
) (AttackPlan, error) {
	contactDistance := source.Plan.ActorFootprintRadius() + targetFootprintRadius
	if zonegeometry.Distance(source.Plan.Position, targetPosition) > contactDistance {
		return AttackPlan{}, ErrControlTargetOutOfRange
	}
	plan, err := PlanAreaAttackWithProfile(source, targetObjectID, targetPosition, profile)
	if err != nil {
		return AttackPlan{}, fmt.Errorf("swipeHit: %w", err)
	}
	return plan, nil
}
