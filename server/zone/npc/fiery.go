package npc

import "github.com/darkspinnet/darkspin/server/game"

func PlanCryosBasicFieryRetaliation(
	npc Snapshot, targetObjectID uint32, targetPosition game.Vec3,
) (AttackPlan, bool) {
	if npc.IsDefeated || npc.HitPoint <= 0 || targetObjectID == 0 {
		return AttackPlan{}, false
	}
	profile, isFound := ActionProfileForNoun(npc.Plan.NounName)
	if !isFound || profile.ModifierName != "CryosBasicFieryBurn" ||
		profile.ModifierDuration <= 0 || profile.ModifierTickDuration <= 0 ||
		profile.ModifierMaximumStack == 0 {
		return AttackPlan{}, false
	}
	return AttackPlan{
		SourceObjectID:   npc.Plan.ObjectID,
		TargetObjectID:   targetObjectID,
		ActionGeneration: npc.ActionGeneration,
		SourcePosition:   npc.Plan.Position,
		TargetPosition:   targetPosition,
		Profile:          profile,
	}, true
}
