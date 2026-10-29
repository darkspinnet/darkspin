package npc

import (
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

// The build-103 subsequent-aggro scripts use the first-aggro template, but
// request reactivation rather than replaying the actor's spawn entrance.
func subsequentAggroProfile(plan SpawnPlan, profile ActionProfile) ActionProfile {
	graph := plan.NPCProfile.AIGraph
	if graph == nil || !graph.IsResolved || graph.SubsequentAggroAbility == nil {
		return profile
	}
	animation := ""
	delay := time.Duration(0)
	switch *graph.SubsequentAggroAbility {
	case "SubsequentAggro_ActivateRobot": // indexed build-103 chunk 731
		animation, delay = "zlm_minn_tc_2_reactivate", 699999988*time.Nanosecond
	case "SubsequentAggro_ActivateSpecialRobot": // chunk 786
		animation, delay = "zlm_lieu_tc_3_reactivate", time.Second
	default:
		return profile
	}
	profile.FirstAggroAbilityName = *graph.SubsequentAggroAbility
	profile.FirstAggroAnimationName = animation
	profile.FirstAggroDelay = delay
	profile.FirstAggroAnimationDelay = 0
	profile.FirstAggroEffectName = ""
	profile.FirstAggroEffectDelay = 0
	profile.FirstAggroRevealDelay = 0
	profile.FirstAggroCinematicDuration = 0
	profile.FirstAggroCinematicRadius = 0
	profile.FirstAggroCinematicOffset = game.Vec3{}
	profile.PreAggroAnimationName = ""
	profile.IsFirstAggroDurationKnown = true
	profile.IsFirstAggroFacingSuppressed = true
	return profile
}

func hasSubsequentAggro(plan SpawnPlan) bool {
	graph := plan.NPCProfile.AIGraph
	if graph == nil || !graph.IsResolved || graph.SubsequentAggroAbility == nil {
		return false
	}
	return *graph.SubsequentAggroAbility == "SubsequentAggro_ActivateRobot" ||
		*graph.SubsequentAggroAbility == "SubsequentAggro_ActivateSpecialRobot"
}

// queueSubsequentAggro is called before a live target is cleared. Action
// completion and direct target transfers do not enter this idle transition.
func queueSubsequentAggro(actor *Snapshot) {
	if actor.TargetObjectID != 0 {
		prepareSubsequentAggro(actor)
	}
}

func prepareSubsequentAggro(actor *Snapshot) {
	if actor.IsDefeated || actor.HitPoint <= 0 || !actor.IsFirstActionStarted ||
		!hasSubsequentAggro(actor.Plan) || actor.Plan.IsSubsequentAggroPending {
		return
	}
	// A canceled primary entrance must finish that same entrance first.
	if actor.IsIntroductionPending() {
		return
	}
	actor.Plan.IsSubsequentAggroPending = true
}

// CompleteSubsequentAggro is called after the admitted reaction wait. A stale
// driver cannot consume the pending reaction belonging to its replacement.
func (e *Session) CompleteSubsequentAggro(objectID uint32, generation uint64) (Snapshot, bool) {
	if e == nil || objectID == 0 || generation == 0 {
		return Snapshot{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || actor.IsDefeated || !actor.IsActionStarted || actor.ActionGeneration != generation ||
		!actor.Plan.IsSubsequentAggroPending {
		return Snapshot{}, false
	}
	actor.Plan.IsSubsequentAggroPending = false
	if !actor.introduction.startedAt.IsZero() {
		actor.introduction.isComplete = true
	}
	e.npcs[objectID] = actor
	return actor, true
}
