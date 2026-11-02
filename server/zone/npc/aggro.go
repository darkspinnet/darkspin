package npc

import (
	"errors"
	"time"
)

func firstAggroAbility(plan SpawnPlan) *string {
	graph := plan.NPCProfile.AIGraph
	if graph == nil || !graph.IsResolved {
		return nil
	}
	if plan.IsStartupInitialized && plan.IsSecondaryStart {
		return graph.FirstAggroAbility2
	}
	return graph.FirstAggroAbility
}

// Chunk 178 draws in Tick, after first-aggro admission. Keep the result on
// the actor so rejected ownership, profile reads and transport retries do not
// consume extra simulator words or change a previously accepted reaction.
func (e *Session) prepareFirstAggro(plan SpawnPlan) (SpawnPlan, error) {
	ability := firstAggroAbility(plan)
	if plan.IsRoarInitialized || ability == nil || *ability != "FirstAggro_Roar33" ||
		plan.IsFixture || plan.IsBoss || plan.IsIntroductionComplete || plan.OwnerObjectID != 0 ||
		plan.Introduction == SpawnIntroductionFloorWarp {
		return plan, nil
	}
	if e.startupRandom == nil {
		return SpawnPlan{}, errors.New("first aggro simulator random unavailable")
	}
	// Native math.random() narrows the MT conversion to float32 before Lua
	// compares it with the authored float32 probability.
	plan.IsRoarSelected = float32(e.startupRandom.Float64()) < float32(0.33)
	plan.IsRoarInitialized = true
	return plan, nil
}

func authoredFirstAggroReaction(plan SpawnPlan, profile ActionProfile) ActionProfile {
	ability := firstAggroAbility(plan)
	if ability == nil || plan.IsIntroductionComplete || plan.IsFixture || plan.IsBoss ||
		plan.OwnerObjectID != 0 || plan.Introduction == SpawnIntroductionFloorWarp ||
		profile.FirstAggroCinematicDuration > 0 {
		return profile
	}
	isRoarSelected := false
	switch *ability {
	case "FirstAggro_Roar33":
		if !plan.IsRoarInitialized {
			return profile
		}
		isRoarSelected = plan.IsRoarSelected
	case "FirstAggro_Roar": // Chunk 994 delegates unconditionally to template 848.
		isRoarSelected = true
	case "FirstAggro_FaceTarget": // Chunk 575 has no animation or wait.
	default:
		return profile
	}
	profile.FirstAggroAbilityName = *ability
	profile.FirstAggroAnimationName = ""
	profile.FirstAggroAnimationDelay = 0
	profile.FirstAggroEffectName = ""
	profile.FirstAggroEffectDelay = 0
	profile.FirstAggroDelay = 0
	profile.FirstAggroRevealDelay = 0
	profile.PreAggroAnimationName = ""
	profile.IsFirstAggroFacingSuppressed = false
	profile.IsFirstAggroDurationKnown = true
	if isRoarSelected {
		profile.FirstAggroAnimationName = "first_aggro_roar"
		profile.FirstAggroDelay = 1750 * time.Millisecond
	}
	return profile
}
