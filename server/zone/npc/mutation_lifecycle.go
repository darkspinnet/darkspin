package npc

import (
	"errors"
	"fmt"
	"time"
)

func isMutationAgent(plan SpawnPlan) bool {
	return plan.NounName == MutationAgentNounName || HasMutationAgentDeath(plan)
}

func isMutationAbilityBlocked(actor Snapshot, at time.Time) bool {
	if actor.IsMutationTransforming() || actor.IsIntroductionPending() || actor.Plan.IsIntroductionHidden ||
		actor.cinematic.isPending || at.Before(actor.cinematic.immobilizedUntil) || at.Before(actor.cinematic.intangibleUntil) {
		return true
	}
	if isStatusBlockedByShield(actor) {
		return false
	}
	return at.Before(actor.status.stunExpiresAt) || at.Before(actor.status.sleepExpiresAt) ||
		at.Before(actor.status.banishExpiresAt) || at.Before(actor.status.intangibleExpiresAt) ||
		at.Before(actor.status.silenceExpiresAt)
}

func beginMutationPassive(actor *Snapshot, at time.Time) {
	state := &actor.Mutation
	rank := max(int32(1), actor.Plan.NPCProfile.NPCRank)
	if actor.Plan.IsEliteModifierApplied() {
		rank++
	}
	state.Rank = uint32(min(int32(3), rank))
	state.CurrentIndex = 1
	if state.Rank > 1 {
		state.CurrentIndex = 2
	}
	state.NextSlotAt = at.Add(time.Second)
	state.NextActionAt = at
	state.NextAuraAt = at.Add(2 * time.Second)
	state.DamageSource = MutationDamageSource{ObjectID: actor.Plan.ObjectID,
		Generation: state.Generation, Faction: actor.Faction, Profile: DamageProfileAt(*actor, at),
		CriticalRating:       actor.Plan.NPCProfile.CriticalRating,
		DifficultyMultiplier: actor.Plan.NPCProfile.DifficultyDamageMultiplier}
	isAccepted := appendMutationEvent(actor, MutationEvent{Kind: MutationEventActivate, At: at, Position: actor.Plan.Position})
	if isAccepted {
		state.IsPassiveActive = true
		state.IsAuraActive = true
	}
}

func advanceMutationSlots(actor *Snapshot, at time.Time) {
	state := &actor.Mutation
	if !state.IsPassiveActive || state.NextSlotAt.IsZero() || at.Before(state.NextSlotAt) {
		return
	}
	state.CurrentIndex = state.CurrentIndex%state.Rank + 1
	state.NextSlotAt = at.Add(time.Second)
}

func (e *Session) mutationSlotAvailable(actor Snapshot) bool {
	index := actor.Mutation.CurrentIndex
	if index == 0 || index > 3 {
		return false
	}
	objectID := actor.Mutation.PromotedObjectIDs[index-1]
	target, isFound := e.npcs[objectID]
	return objectID == 0 || !isFound || !target.IsPublished || target.IsDefeated || target.HitPoint <= 0
}

func (e *Session) mutationBeamTarget(actor Snapshot, at time.Time) (Snapshot, bool) {
	beam := actor.Mutation.Beam
	target, isFound := e.npcs[beam.TargetObjectID]
	return target, isFound && target.IsPublished && !target.IsDefeated && target.HitPoint > 0 &&
		target.Mutation.Generation == beam.TargetGeneration && target.Faction == beam.Faction &&
		!at.Before(target.status.intangibleExpiresAt) && !at.Before(target.status.banishExpiresAt) &&
		!at.Before(target.cinematic.intangibleUntil)
}

func stopMutationBeam(actor *Snapshot, at time.Time) bool {
	if !actor.Mutation.Beam.IsActive {
		return true
	}
	isAccepted := appendMutationEvent(actor, MutationEvent{Kind: MutationEventBeamStop, At: at,
		TargetObjectID: actor.Mutation.Beam.TargetObjectID, TargetGeneration: actor.Mutation.Beam.TargetGeneration})
	if !isAccepted {
		return false
	}
	actor.Mutation.Beam = MutationBeamState{}
	actor.Mutation.NextActionAt = at
	return true
}

func stopMutationLife(actor *Snapshot, at time.Time) {
	state := &actor.Mutation
	if !state.IsPassiveActive && !state.IsAuraActive && !state.Beam.IsActive && !state.IsDeactivatePending {
		return
	}
	state.IsPassiveActive = false
	state.IsAuraActive = false
	state.NextSlotAt = time.Time{}
	state.NextAuraAt = time.Time{}
	state.AuraHitAt = time.Time{}
	// An appended hit represents the resumed no-yield query/damage callback.
	// Destruction cancels future waits, not an already accepted hit operation.
	state.IsDeactivatePending = true
	isStopped := stopMutationBeam(actor, at)
	if !isStopped {
		return
	}
	isAccepted := appendMutationEvent(actor, MutationEvent{Kind: MutationEventDeactivate, At: at})
	if isAccepted {
		state.IsDeactivatePending = false
	}
}

func flushMutationRetirement(actor *Snapshot, at time.Time) bool {
	retirement := &actor.Mutation.Retirement
	if retirement.IsBeamPending {
		isAccepted := appendMutationEventGeneration(actor, MutationEvent{
			Kind: MutationEventBeamStop, At: at, TargetObjectID: retirement.BeamTargetObjectID,
			TargetGeneration: retirement.BeamTargetGeneration}, retirement.Generation)
		if !isAccepted {
			return false
		}
		retirement.IsBeamPending = false
	}
	if retirement.IsDeactivatePending {
		isAccepted := appendMutationEventGeneration(actor, MutationEvent{
			Kind: MutationEventDeactivate, At: at}, retirement.Generation)
		if !isAccepted {
			return false
		}
		retirement.IsDeactivatePending = false
	}
	actor.Mutation.Retirement = MutationRetirementState{}
	return true
}

func (e *Session) advanceMutationBeam(actor *Snapshot, req MutationAdvanceRequest) {
	state := &actor.Mutation
	beam := &state.Beam
	if !beam.IsActive || state.EventCount >= 6 {
		return
	}
	if isMutationAbilityBlocked(*actor, req.At) {
		stopMutationBeam(actor, req.At)
		return
	}
	if !beam.IsChannelActive {
		if req.At.Before(beam.ChannelAt) {
			return
		}
		beam.Faction = actor.Faction
		target, isValid := e.mutationBeamTarget(*actor, req.At)
		if !isValid {
			stopMutationBeam(actor, req.At)
			return
		}
		isAccepted := appendMutationEvent(actor, MutationEvent{Kind: MutationEventBeamChannel,
			At: req.At, TargetObjectID: target.Plan.ObjectID, TargetGeneration: target.Mutation.Generation})
		if !isAccepted {
			return
		}
		beam.IsChannelActive = true
	}
	if !beam.IsPromoted {
		target, isValid := e.mutationBeamTarget(*actor, req.At)
		if !isValid {
			stopMutationBeam(actor, req.At)
			return
		}
		if req.At.Before(beam.MutateAt) {
			return
		}
		advanceMutationSlots(actor, req.At)
		if !target.Plan.IsEliteModifierApplied() && !target.IsMutationTransforming() {
			if target.Mutation.EventCount >= uint32(len(target.Mutation.Events)) {
				return
			}
			isAccepted := e.beginMutationTransform(target.Plan.ObjectID, actor.Plan.ObjectID, req.At)
			if !isAccepted {
				// The authored caller ignores RequestModifier rejection and
				// writes its current slot even when admission fails.
			}
			state.PromotedObjectIDs[state.CurrentIndex-1] = target.Plan.ObjectID
		}
		beam.IsPromoted = true
	}
	if !req.At.Before(beam.ReleaseAt) {
		stopMutationBeam(actor, req.At)
	}
}

func (e *Session) AdvanceMutationAgents(req MutationAdvanceRequest) ([]MutationEvent, error) {
	if e == nil || req.At.IsZero() {
		return nil, fmt.Errorf("mutation advance invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	advanceErrors := make([]error, 0)
	for _, objectID := range e.objectIDs {
		actor := e.npcs[objectID]
		if !isMutationAgent(actor.Plan) {
			continue
		}
		isRetired := flushMutationRetirement(&actor, req.At)
		if !isRetired {
			e.npcs[objectID] = actor
			continue
		}
		if !actor.IsPublished || actor.IsDefeated || actor.HitPoint <= 0 {
			stopMutationLife(&actor, req.At)
			e.npcs[objectID] = actor
			continue
		}
		if actor.Mutation.EventCount >= 6 {
			continue
		}
		if !actor.Mutation.IsPassiveActive {
			beginMutationPassive(&actor, req.At)
		}
		e.advanceMutationBeam(&actor, req)
		advanceMutationSlots(&actor, req.At)
		advanceMutationAura(&actor, req.At)
		isApproaching := false
		if !actor.Mutation.Beam.IsActive && !isMutationAbilityBlocked(actor, req.At) && actor.Mutation.EventCount < 6 &&
			!req.At.Before(actor.Mutation.NextActionAt) && e.mutationSlotAvailable(actor) {
			target, isFound := e.mutationCandidate(actor, req)
			if isFound {
				isApproaching = true
				isInRange, err := e.approachMutationCandidate(&actor, target, req)
				if err != nil {
					e.npcs[objectID] = actor
					advanceErrors = append(advanceErrors, fmt.Errorf("mutationApproach[%d]: %w", objectID, err))
					continue
				}
				if !isInRange {
					e.npcs[objectID] = actor
					continue
				}
				isAccepted := appendMutationEvent(&actor, MutationEvent{Kind: MutationEventBeamStart,
					At: req.At, TargetObjectID: target.Plan.ObjectID, TargetGeneration: target.Mutation.Generation})
				if isAccepted {
					actor.Mutation.Beam = MutationBeamState{TargetObjectID: target.Plan.ObjectID,
						TargetGeneration: target.Mutation.Generation, StartedAt: req.At,
						ChannelAt: req.At.Add(mutationBeamHitDelay), MutateAt: req.At.Add(2500 * time.Millisecond),
						ReleaseAt: req.At.Add(3 * time.Second), IsActive: true}
				}
			}
		}
		if !isApproaching && actor.Mutation.EventCount < 6 {
			err := e.advanceMutationMovement(&actor, req)
			if err != nil {
				e.npcs[objectID] = actor
				advanceErrors = append(advanceErrors, fmt.Errorf("mutationMove[%d]: %w", objectID, err))
				continue
			}
		}
		e.npcs[objectID] = actor
	}
	err := e.advanceMutationTransforms(req.At)
	if err != nil {
		advanceErrors = append(advanceErrors, fmt.Errorf("mutationTransform: %w", err))
	}
	if len(advanceErrors) > 0 {
		return e.mutationEventsLocked(), fmt.Errorf("mutationAdvance: %w", errors.Join(advanceErrors...))
	}
	return e.mutationEventsLocked(), nil
}

func (e *Session) NextMutationDeadline() time.Time {
	if e == nil {
		return time.Time{}
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	var deadline time.Time
	for _, objectID := range e.objectIDs {
		actor := e.npcs[objectID]
		state := actor.Mutation
		if state.EventCount > 0 || state.IsDeactivatePending || state.Retirement.IsDeactivatePending || state.Retirement.IsBeamPending {
			return time.Now()
		}
		var beamAt time.Time
		if state.Beam.IsActive {
			beamAt = state.Beam.ChannelAt
			if state.Beam.IsChannelActive {
				beamAt = state.Beam.MutateAt
			}
			if state.Beam.IsPromoted {
				beamAt = state.Beam.ReleaseAt
			}
		}
		auraAt := state.NextAuraAt
		if !state.AuraHitAt.IsZero() {
			auraAt = state.AuraHitAt
		}
		var movementAt time.Time
		if state.IsPassiveActive && !state.Movement.IsPaused {
			movementAt = state.Movement.CheckAt
		}
		var healAt time.Time
		if state.Transform.IsFullHealPending && actor.IsPublished && !actor.IsDefeated && actor.HitPoint > 0 {
			healAt = state.Transform.HealAfter.Add(time.Nanosecond)
		}
		deadlines := [...]time.Time{state.NextSlotAt, auraAt, beamAt,
			state.Transform.CompleteAt, healAt, movementAt}
		for _, candidate := range deadlines {
			if !candidate.IsZero() && (deadline.IsZero() || candidate.Before(deadline)) {
				deadline = candidate
			}
		}
	}
	return deadline
}
