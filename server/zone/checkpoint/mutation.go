package checkpoint

import (
	"errors"
	"fmt"
	"math"
	"time"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// RebaseMutation freezes process downtime while retaining overdue stages and
// zero-time sentinels. Actor-life epochs and accepted event identities persist.
func RebaseMutation(state zonenpc.MutationState, capturedAt, restoredAt time.Time) (zonenpc.MutationState, error) {
	if state == (zonenpc.MutationState{}) {
		return state, nil
	}
	if capturedAt.IsZero() || restoredAt.IsZero() {
		return zonenpc.MutationState{}, errors.New("mutation clock unavailable")
	}
	if state.EventCount > uint32(len(state.Events)) {
		return zonenpc.MutationState{}, errors.New("mutation event count invalid")
	}
	state.NextSlotAt = rebaseMutationTime(state.NextSlotAt, capturedAt, restoredAt)
	state.NextActionAt = rebaseMutationTime(state.NextActionAt, capturedAt, restoredAt)
	state.NextAuraAt = rebaseMutationTime(state.NextAuraAt, capturedAt, restoredAt)
	state.AuraHitAt = rebaseMutationTime(state.AuraHitAt, capturedAt, restoredAt)
	state.Beam.StartedAt = rebaseMutationTime(state.Beam.StartedAt, capturedAt, restoredAt)
	state.Beam.ChannelAt = rebaseMutationTime(state.Beam.ChannelAt, capturedAt, restoredAt)
	state.Beam.MutateAt = rebaseMutationTime(state.Beam.MutateAt, capturedAt, restoredAt)
	state.Beam.ReleaseAt = rebaseMutationTime(state.Beam.ReleaseAt, capturedAt, restoredAt)
	state.Transform.CompleteAt = rebaseMutationTime(state.Transform.CompleteAt, capturedAt, restoredAt)
	state.Transform.HealAfter = rebaseMutationTime(state.Transform.HealAfter, capturedAt, restoredAt)
	state.Movement.UpdatedAt = rebaseMutationTime(state.Movement.UpdatedAt, capturedAt, restoredAt)
	state.Movement.CheckAt = rebaseMutationTime(state.Movement.CheckAt, capturedAt, restoredAt)
	for index := range state.Events {
		state.Events[index].At = rebaseMutationTime(state.Events[index].At, capturedAt, restoredAt)
	}
	// A prior-process pulse has no live recipient. Retire that one-shot while
	// retaining separate AuraHit obligations and their original event IDs.
	retainedCount := uint32(0)
	for index := uint32(0); index < state.EventCount; index++ {
		if state.Events[index].Kind == zonenpc.MutationEventAuraPulse {
			continue
		}
		state.Events[retainedCount] = state.Events[index]
		retainedCount++
	}
	clear(state.Events[retainedCount:])
	state.EventCount = retainedCount
	return state, nil
}

func rebaseMutationTime(at, capturedAt, restoredAt time.Time) time.Time {
	if at.IsZero() {
		return time.Time{}
	}
	return restoredAt.Add(at.Sub(capturedAt))
}

func ValidateMutation(npc NPC) error {
	state := npc.State.Mutation
	if state == (zonenpc.MutationState{}) {
		return nil
	}
	if npc.MutationCapturedAt.IsZero() || state.EventCount > uint32(len(state.Events)) ||
		state.Rank > uint32(len(state.PromotedObjectIDs)) || state.CurrentIndex > uint32(len(state.PromotedObjectIDs)) {
		return errors.New("mutation checkpoint shape invalid")
	}
	err := zonenpc.ValidateMutationGrowth(state.Growth)
	if err != nil {
		return fmt.Errorf("mutationGrowth: %w", err)
	}
	if (state.Retirement.IsBeamPending || state.Retirement.IsDeactivatePending) &&
		(state.Retirement.Generation == 0 || state.Retirement.Generation > state.Generation) {
		return errors.New("mutation retirement generation invalid")
	}
	previousSequence := uint64(0)
	for index := uint32(0); index < state.EventCount; index++ {
		event := state.Events[index]
		if event.ID.ObjectID != npc.State.Plan.ObjectID || event.ID.Generation == 0 || event.ID.Generation > state.Generation ||
			event.ID.Sequence <= previousSequence || event.ID.Sequence > state.NextSequence || event.At.IsZero() ||
			event.Kind < zonenpc.MutationEventActivate || event.Kind > zonenpc.MutationEventFullHeal {
			return fmt.Errorf("mutationEvent[%d]: invalid", index)
		}
		err = validateMutationDamageEvent(event)
		if err != nil {
			return fmt.Errorf("mutationDamage[%d]: %w", index, err)
		}
		previousSequence = event.ID.Sequence
	}
	return nil
}

func validateMutationDamageEvent(event zonenpc.MutationEvent) error {
	work := event.DamageWork
	if work == nil {
		if event.IsDamageClaimed {
			return errors.New("claimed damage work missing")
		}
		return nil
	}
	if event.Kind != zonenpc.MutationEventAuraHit || !event.IsDamageClaimed || !work.IsPrepared ||
		work.TargetCount > uint32(len(work.Targets)) {
		return errors.New("damage work shape invalid")
	}
	// Runtime shield/transition receipts are not durable. Keep the previous
	// checkpoint until every prepared target has finished its continuation.
	if !work.IsComplete() {
		return errors.New("damage work unfinished")
	}
	previousObjectID := uint32(0)
	for index := uint32(0); index < work.TargetCount; index++ {
		target := work.Targets[index]
		if target.ObjectID <= previousObjectID || target.UserID == 0 || target.PeerGeneration == 0 ||
			!isMutationDamageScalarValid(target.Damage) || !isMutationDamageScalarValid(target.PreviousHitPoint) ||
			!isMutationDamageScalarValid(target.HitPoint) {
			return fmt.Errorf("damageTarget[%d]: invalid", index)
		}
		previousObjectID = target.ObjectID
	}
	return nil
}

func isMutationDamageScalarValid(scalar float32) bool {
	return scalar >= 0 && !math.IsNaN(float64(scalar)) && !math.IsInf(float64(scalar), 0)
}
