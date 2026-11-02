package npc

import (
	"strings"
	"time"
)

// EffectivePassiveRank retains the rank captured when the AI passive starts.
// Later promotion cannot rewrite an already created modifier instance.
func EffectivePassiveRank(plan SpawnPlan) int32 {
	if plan.NPCPassiveRank > 0 {
		return max(int32(1), min(int32(3), plan.NPCPassiveRank))
	}
	return EffectiveNPCRank(plan)
}

func hasZelemMeleePassive(plan SpawnPlan) bool {
	if plan.NPCProfile.AIGraph != nil && plan.NPCProfile.AIGraph.IsResolved {
		passive := plan.NPCProfile.AIGraph.PassiveAbility
		return passive != nil && strings.EqualFold(*passive, "ZelemBasicMeleePassive")
	}
	return nounSpecies(plan.NounName) == "zelembasicmelee"
}

func ZelemMeleeAOEResistance(plan SpawnPlan) float32 {
	if !hasZelemMeleePassive(plan) {
		return 0
	}
	return [...]float32{0.75, 0.8, 0.9}[EffectivePassiveRank(plan)-1]
}

func NomadSpecialThreePassiveDefense(plan SpawnPlan) float32 {
	return [...]float32{250, 500, 750}[EffectivePassiveRank(plan)-1]
}

func NomadSpecialThreeTurtleProfileForPlan(plan SpawnPlan) (ActionProfile, bool) {
	profile, isFound := NomadSpecialThreeTurtleProfile(plan.NounName)
	if !isFound {
		return ActionProfile{}, false
	}
	profile.NumberOfTicks = uint32(5 + EffectivePassiveRank(plan))
	profile.AbilityRank = EffectivePassiveRank(plan)
	return profile, true
}

func NomadWithDroneShieldDurationForPlan(plan SpawnPlan) (time.Duration, bool) {
	duration, isFound := NomadWithDroneShieldDuration(plan.NounName)
	if !isFound {
		return duration, false
	}
	return [...]time.Duration{5 * time.Second, 6500 * time.Millisecond, 8 * time.Second}[EffectivePassiveRank(plan)-1], true
}

func areaShiftMovementScale(actor Snapshot, at time.Time) float32 {
	if !at.Before(actor.status.areaShiftExpiresAt) {
		return 1
	}
	if !hasZelemMeleePassive(actor.Plan) {
		return 0.5
	}
	baseScale := max(float32(0.05), 1+EffectiveMovementSpeedBuff(actor.Plan))
	return max(float32(0.05), baseScale-0.5) / baseScale
}
