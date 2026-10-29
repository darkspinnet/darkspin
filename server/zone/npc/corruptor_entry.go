package npc

import (
	"errors"
	"strings"
	"time"
)

func IsCorruptorStageTwoNoun(nounName string) bool {
	switch strings.ToLower(nounName) {
	case "scaldronboss_stage2.noun", "scaldronboss_stage2_2.noun", "scaldronboss_stage2_3.noun":
		return true
	default:
		return false
	}
}

func IsDirectCorruptorStageTwo(plan SpawnPlan) bool {
	graph := plan.NPCProfile.AIGraph
	return IsCorruptorStageTwoNoun(plan.NounName) && plan.OwnerObjectID == 0 &&
		plan.NPCProfile.IsClassKnown && graph != nil && graph.IsResolved &&
		graph.DefinitionID == 2682360449 && graph.FirstAggroAbility != nil &&
		*graph.FirstAggroAbility == "SecondAggro_ScaldronBoss" &&
		graph.PassiveAbility != nil && *graph.PassiveAbility == "ScaldronBoss_AIPassiveStage2"
}

func (e Snapshot) IsSecondAggroPending() bool {
	return e.IsCorruptorStageTwo && e.cinematic.isPending
}

// An already-created second-stage noun has no preceding corpse or replacement
// birth. Reserve its real camera/intangible owner without replaying stage one.
func (e *Session) BeginDirectCorruptor(objectID uint32, at time.Time) (Snapshot, error) {
	if e == nil || objectID == 0 || at.IsZero() {
		return Snapshot{}, errors.New("direct corruptor context unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || actor.IsDefeated || !actor.IsPublished || actor.IsBirthPending ||
		actor.IsFirstActionStarted || !IsDirectCorruptorStageTwo(actor.Plan) {
		return Snapshot{}, errors.New("direct corruptor admission unavailable")
	}
	actor.IsCorruptorStageTwo = true
	actor.IsFirstActionStarted = true
	actor.Plan.IsIntroductionHidden = false
	actor.Plan.IsIntroductionComplete = true
	actor.Plan.ActionProfile = corruptorPoseProfile(0.5)
	actor.Plan.IsActionKnown = true
	actor.corruptorCombat = corruptorCombat{specialProfile: actor.Plan.ActionProfile.Clone()}
	actor.cinematic.generation++
	if actor.cinematic.generation == 0 {
		actor.cinematic.generation = 1
	}
	actor.cinematic.isPending = true
	actor.cinematic.intangibleUntil = at.Add(SecondAggroCinematicDuration)
	actor.cinematic.focus = SecondAggroCinematicFocus(actor.Plan.Position)
	actor.cinematic.deathEndsAt = at
	actor.cinematic.deathNounName = ""
	e.npcs[objectID] = actor
	return actor, nil
}
