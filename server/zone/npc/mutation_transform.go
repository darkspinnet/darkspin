package npc

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// MutationGrowthState preserves the independent growth contribution when a
// promoted actor is captured without its private runtime status fields.
type MutationGrowthState struct {
	StackCount          uint32
	BaseMaximumHitPoint float32
	BaseGraphicsScale   float32
}

func captureMutationGrowth(target *Snapshot) {
	if !target.Plan.IsEliteModifierApplied() {
		return
	}
	target.Mutation.Growth = MutationGrowthState{
		StackCount:          target.status.oozeGrowthStack,
		BaseMaximumHitPoint: target.status.oozeBaseMaximumHitPoint,
		BaseGraphicsScale:   target.status.oozeBaseGraphicsScale,
	}
}

func ValidateMutationGrowth(state MutationGrowthState) error {
	if state.StackCount > oozeGrowthMaximumStack ||
		state.BaseMaximumHitPoint < 0 || state.BaseGraphicsScale < 0 ||
		math.IsNaN(float64(state.BaseMaximumHitPoint)) || math.IsInf(float64(state.BaseMaximumHitPoint), 0) ||
		math.IsNaN(float64(state.BaseGraphicsScale)) || math.IsInf(float64(state.BaseGraphicsScale), 0) {
		return errors.New("mutation growth snapshot invalid")
	}
	if state.StackCount > 0 && state.BaseMaximumHitPoint <= 0 {
		return errors.New("mutation growth baseline missing")
	}
	if state.StackCount == 0 && (state.BaseMaximumHitPoint != 0 || state.BaseGraphicsScale != 0) {
		return errors.New("mutation growth baseline unexpected")
	}
	return nil
}

func restoreMutationGrowth(target *Snapshot) error {
	state := target.Mutation.Growth
	err := ValidateMutationGrowth(state)
	if err != nil {
		return fmt.Errorf("growthValidate: %w", err)
	}
	if state.StackCount == 0 {
		return nil
	}
	target.status.oozeGrowthStack = state.StackCount
	target.status.oozeBaseMaximumHitPoint = state.BaseMaximumHitPoint
	target.status.oozeBaseGraphicsScale = state.BaseGraphicsScale
	target.status.oozeGrowthDamageIncrease = 0.25 * float32(state.StackCount)
	return nil
}

// IsMutationTransforming is owned by the target modifier, independently of
// the initiating agent's life or beam. Advancement releases it explicitly.
func (e Snapshot) IsMutationTransforming() bool {
	return e.Mutation.Transform.IsActive
}

func (e Snapshot) IsMutationRandomTeleportImmune() bool {
	return e.IsMutationTransforming()
}

func (e Snapshot) IsMutationBanishImmune() bool {
	return e.IsMutationTransforming()
}

func (e Snapshot) IsMutationKnockbackImmune() bool {
	return e.IsMutationTransforming()
}

// The retained ooze base includes permanent percent modifiers but excludes
// the replaceable growth percent. Native sums those percents against one base.
func (e SpawnPlan) permanentHealthMultiplier() float32 {
	multiplier := float32(1)
	if e.IsEliteModifierApplied() {
		multiplier += EliteBonusForType(e.NPCProfile.NPCType).Health
	}
	if e.IsCaptain {
		multiplier += captainHealthBonus
		if e.BossIdentity.HasModifier(toughModifierName) {
			multiplier += ToughHealthBonusForType(e.NPCProfile.NPCType)
		}
	}
	return multiplier
}

func (e Snapshot) MutationBodyScale() float32 {
	return e.Plan.PermanentBodyScale() + 0.1*float32(e.status.oozeGrowthStack)
}

// beginMutationTransform is called with Session.mu held by the mutation
// lifecycle. The target owns its one-second modifier after acceptance.
func (e *Session) beginMutationTransform(targetID, sourceID uint32, at time.Time) bool {
	if targetID == 0 || sourceID == 0 || at.IsZero() {
		return false
	}
	target, isTargetFound := e.npcs[targetID]
	source, isSourceFound := e.npcs[sourceID]
	if !isTargetFound || !isSourceFound || !source.IsPublished || source.IsDefeated || source.HitPoint <= 0 ||
		!target.IsPublished || target.IsDefeated ||
		target.HitPoint <= 0 || target.IsMutationTransforming() ||
		target.Plan.IsEliteModifierApplied() {
		return false
	}
	// The NPC runtime has explicit owners for these stronger entrance/scene
	// holds. Do not replace their animation or invalidate their action lease.
	if target.cinematic.isPending || at.Before(target.cinematic.intangibleUntil) ||
		at.Before(target.cinematic.immobilizedUntil) || target.IsIntroductionPending() ||
		target.Plan.IsIntroductionHidden {
		return false
	}
	target.Mutation.Transform = MutationTransformState{
		SourceObjectID: sourceID, SourceGeneration: source.Mutation.Generation,
		CompleteAt: at.Add(time.Second), IsActive: true,
	}
	target.IsActionStarted = false
	target.ActionOwner = ActionOwner{}
	target.ActionGeneration++
	if target.ActionGeneration == 0 {
		target.ActionGeneration = 1
	}
	isAccepted := appendMutationEvent(&target, MutationEvent{
		Kind: MutationEventTransformStart, At: at, TargetObjectID: targetID,
		TargetGeneration: target.Mutation.Generation, Position: target.Plan.Position,
	})
	if !isAccepted {
		return false
	}
	e.npcs[targetID] = target
	return true
}

// advanceMutationTransforms runs once before the source agent state machines.
// FullHeal resumes only on a later advance, preserving the authored yield.
func (e *Session) advanceMutationTransforms(at time.Time) error {
	if at.IsZero() {
		return errors.New("mutation transform time unavailable")
	}
	transformErrors := make([]error, 0)
	for _, objectID := range e.objectIDs {
		target := e.npcs[objectID]
		transform := target.Mutation.Transform
		if !transform.IsActive && !transform.IsFullHealPending {
			continue
		}
		if !target.IsPublished {
			target.Mutation.Transform = MutationTransformState{}
			e.npcs[objectID] = target
			continue
		}
		if target.IsDefeated || target.HitPoint <= 0 {
			if transform.IsActive {
				// Lua336 is OnAgentDeath; Lua650's later OnAgentDestroyed
				// full-heal lifetime must not be conflated with this phase.
				target.Mutation.Transform = MutationTransformState{}
				e.npcs[objectID] = target
			}
			// Native FullHeal can invoke restoration callbacks at zero HP.
			// Retain that pending intent; do not invent a resurrection here.
			continue
		}
		if transform.IsFullHealPending {
			if !at.After(transform.HealAfter) {
				continue
			}
			target.HitPoint = target.Plan.NPCProfile.HitPoint
			target.Mutation.Transform.IsFullHealPending = false
			target.Mutation.Transform.HealAfter = time.Time{}
			isAccepted := appendMutationEvent(&target, MutationEvent{
				Kind: MutationEventFullHeal, At: at, TargetObjectID: objectID,
				TargetGeneration: target.Mutation.Generation, Position: target.Plan.Position,
			})
			if isAccepted {
				e.npcs[objectID] = target
			}
			continue
		}
		if at.Before(transform.CompleteAt) {
			continue
		}
		isFullHealth := target.HitPoint == target.Plan.NPCProfile.HitPoint
		isEliteApplied := target.Plan.IsEliteModifierApplied()
		if !isEliteApplied {
			err := applyMutationElite(&target)
			if err != nil {
				transformErrors = append(transformErrors, fmt.Errorf("transformElite[%d]: %w", objectID, err))
				continue
			}
		}
		target.Mutation.Transform.IsActive = false
		target.Mutation.Transform.CompleteAt = time.Time{}
		target.Mutation.Transform.IsFullHealPending = !isEliteApplied && isFullHealth
		if target.Mutation.Transform.IsFullHealPending {
			target.Mutation.Transform.HealAfter = at
		}
		isAccepted := appendMutationEvent(&target, MutationEvent{
			Kind: MutationEventTransformComplete, At: at, TargetObjectID: objectID,
			TargetGeneration: target.Mutation.Generation, Position: target.Plan.Position,
		})
		if isAccepted {
			e.npcs[objectID] = target
		}
	}
	err := errors.Join(transformErrors...)
	if err != nil {
		return fmt.Errorf("transformAdvance: %w", err)
	}
	return nil
}

// MakeElite's server business hook is stubbed in the retained clients. This
// content-backed compatibility operation applies Lua650's proven modifiers.
func applyMutationElite(target *Snapshot) error {
	maximumHitPoint := target.Plan.NPCProfile.HitPoint
	bonus := EliteBonusForType(target.Plan.NPCProfile.NPCType).Health
	permanentMultiplier := target.Plan.permanentHealthMultiplier()
	if target.status.oozeBaseMaximumHitPoint > 0 {
		base := target.status.oozeBaseMaximumHitPoint / permanentMultiplier
		maximumHitPoint = base * (permanentMultiplier + bonus + 0.25*float32(target.status.oozeGrowthStack))
		target.status.oozeBaseMaximumHitPoint = base * (permanentMultiplier + bonus)
	} else {
		maximumHitPoint *= 1 + bonus
	}
	if maximumHitPoint <= 0 || math.IsNaN(float64(maximumHitPoint)) || math.IsInf(float64(maximumHitPoint), 0) {
		return errors.New("mutation elite maximum invalid")
	}
	target.Plan.NPCProfile.HitPoint = maximumHitPoint
	target.Plan.IsElite = true
	captureMutationGrowth(target)
	return nil
}
