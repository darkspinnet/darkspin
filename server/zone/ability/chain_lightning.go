package ability

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type ChainLightningHopRequest struct {
	Plan             BasicPlan
	Creature         game.GameplayCreature
	PreviousPosition game.Vec3
	VisitedObjectIDs []uint32
	HopIndex         uint32
}

// PlanChainLightningHop follows TargetUtils chunk406 prototypes 0 and 2:
// nearest unvisited hostile within the leap radius, using the previous target's
// position captured before the wait. Equal distances use object ID for stability.
func PlanChainLightningHop(
	enemies TargetReader, req ChainLightningHopRequest,
) (BasicPlan, zonenpc.Snapshot, bool, error) {
	plan := req.Plan
	chain := plan.Definition.ChainLightning
	if enemies == nil || chain.MaximumHopCount == 0 ||
		req.HopIndex == 0 || req.HopIndex > chain.MaximumHopCount {
		return BasicPlan{}, zonenpc.Snapshot{}, false, nil
	}
	var target zonenpc.Snapshot
	isFound := false
	nearestDistance := chain.LeapRadius
	for _, enemy := range enemies.LiveSnapshots() {
		if !IsHostileMeleeTarget(enemy) || enemy.Plan.ObjectID == plan.SourceObjectID {
			continue
		}
		isVisited := false
		for _, objectID := range req.VisitedObjectIDs {
			if objectID == enemy.Plan.ObjectID {
				isVisited = true
				break
			}
		}
		if isVisited {
			continue
		}
		distance := positionDistance(req.PreviousPosition, enemy.Plan.Position)
		if distance > nearestDistance ||
			(isFound && distance == nearestDistance && enemy.Plan.ObjectID > target.Plan.ObjectID) {
			continue
		}
		target = enemy
		nearestDistance = distance
		isFound = true
	}
	if !isFound {
		return BasicPlan{}, zonenpc.Snapshot{}, false, nil
	}
	// OnChainHit scales the authored interval, then supplies the unchanged
	// coefficient to TakeDamage. Project after scaling so additive attack bonuses
	// and the final integer rounding retain their authored order.
	multiplier := 1 + chain.DamageIncrease*float32(req.HopIndex)
	damage, err := ProjectDamage(req.Creature, plan.Definition,
		plan.Definition.MinimumDamage*multiplier,
		plan.Definition.MaximumDamage*multiplier)
	if err != nil {
		return BasicPlan{}, zonenpc.Snapshot{}, false, fmt.Errorf("chainDamage: %w", err)
	}
	plan.Damage = damage
	plan.TargetObjectID = target.Plan.ObjectID
	return plan, target, true, nil
}
