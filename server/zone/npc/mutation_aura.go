package npc

import "time"

// advanceMutationAura retains the startup snapshot and pre-wait position.
// A late poll resumes a coroutine step without manufacturing missed pulses.
func advanceMutationAura(actor *Snapshot, at time.Time) {
	state := &actor.Mutation
	if !state.IsAuraActive || state.EventCount >= 6 {
		return
	}
	if !state.AuraHitAt.IsZero() {
		if at.Before(state.AuraHitAt) {
			return
		}
		isAccepted := appendMutationEvent(actor, MutationEvent{Kind: MutationEventAuraHit,
			At: at, Position: state.AuraPosition, Facing: state.AuraFacing, DamageSource: state.DamageSource})
		if isAccepted {
			state.AuraHitAt = time.Time{}
			state.NextAuraAt = at.Add(2 * time.Second)
		}
		return
	}
	if at.Before(state.NextAuraAt) {
		return
	}
	isAccepted := appendMutationEvent(actor, MutationEvent{Kind: MutationEventAuraPulse,
		At: at, Position: actor.Plan.Position, Facing: actor.Facing, DamageSource: state.DamageSource})
	if isAccepted {
		state.AuraPosition = actor.Plan.Position
		state.AuraFacing = actor.Facing
		state.AuraHitAt = at.Add(mutationAuraHitDelay)
	}
}
