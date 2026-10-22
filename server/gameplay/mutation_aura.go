package gameplay

import (
	"errors"
	"fmt"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e gameplayPendingRuntime) pollMutationAgentsLocked(member *gameplayPeerSession, sourceTime uint64) ([][]byte, error) {
	sessionKey := ""
	for key, current := range e.registry.sessions {
		if gameplaySessionMemberKey(current) == gameplaySessionMemberKey(*member) && current.generation == member.generation && current.transportGeneration == member.transportGeneration {
			sessionKey = key
			e.registry.sessions[key] = *member
			break
		}
	}
	events, advanceErr := member.zone.AdvanceMutationAgents(e.now())
	if advanceErr != nil {
		// A later source may fail after earlier sources retained publications.
		// Hand off every accepted outbox entry before reporting that failure.
		events = member.zone.NPCs().MutationEvents()
	}
	mutationErrors := make([]error, 0)
	if advanceErr != nil {
		mutationErrors = append(mutationErrors, fmt.Errorf("mutationAdvance: %w", advanceErr))
	}
	for _, event := range events {
		switch event.Kind {
		case zonenpc.MutationEventActivate, zonenpc.MutationEventDeactivate:
			e.registry.queueMutationEventLocked(member.zone, event, mutationSourceFrame)
		case zonenpc.MutationEventBeamChannel, zonenpc.MutationEventBeamStop:
			e.registry.queueMutationEventLocked(member.zone, event, mutationBeamFrame)
			if event.Kind == zonenpc.MutationEventBeamStop {
				e.registry.queueMutationEventLocked(member.zone, event, mutationMovementFrame)
			}
		case zonenpc.MutationEventTransformStart:
			e.registry.queueMutationEventLocked(member.zone, event, mutationTransformFrame)
		case zonenpc.MutationEventTransformComplete:
			e.registry.queueMutationEliteLocked(member.zone, event.ID.ObjectID, true)
		case zonenpc.MutationEventFullHeal:
			// Lua650 resumes after its yield; the permanent attachment already
			// belongs to this target. Publish only its current stats here.
			e.registry.queueMutationEliteLocked(member.zone, event.ID.ObjectID, false)
		case zonenpc.MutationEventAuraPulse:
			e.registry.queueMutationEventLocked(member.zone, event, mutationPulseFrame)
		case zonenpc.MutationEventMovement:
			e.registry.queueMutationEventLocked(member.zone, event, mutationMovementFrame)
		case zonenpc.MutationEventBeamStart:
			// The authored PlayAnimationState override is empty.
			e.registry.queueMutationEventLocked(member.zone, event, mutationMovementFrame)
		case zonenpc.MutationEventAuraHit:
			if sessionKey != "" {
				*member = e.registry.sessions[sessionKey]
			}
			packets, damageErr := e.applyMutationAuraDamageLocked(member, event, sourceTime)
			e.retainMutationDamageMemberLocked(member)
			work, isWorkFound := member.zone.NPCs().MutationDamageWork(event.ID)
			if isWorkFound && len(packets) != 0 {
				e.registry.queueMutationFeedbackLocked(member.zone, work, packets)
				e.acceptMutationAuraDamagePacketsLocked(member.zone, event.ID)
			}
			if sessionKey != "" {
				*member = e.registry.sessions[sessionKey]
			}
			if damageErr != nil {
				mutationErrors = append(mutationErrors, fmt.Errorf("mutationAuraDamage: %w", damageErr))
			}
			if !isWorkFound {
				mutationErrors = append(mutationErrors, errors.New("mutation damage ledger unavailable"))
				continue
			}
			if damageErr != nil || !work.IsComplete() {
				continue
			}
			isCommitted := member.zone.NPCs().CommitMutationEvent(event.ID)
			if isCommitted {
				e.releaseMutationAuraDamageLocked(member.zone, event.ID)
			}
			continue
		default:
			continue
		}
		isCommitted := member.zone.NPCs().CommitMutationEvent(event.ID)
		if !isCommitted && e.logger != nil {
			e.logger.Printf("Mutation event handoff superseded object=%d sequence=%d", event.ID.ObjectID, event.ID.Sequence)
		}
	}
	if sessionKey != "" {
		*member = e.registry.sessions[sessionKey]
	}
	mutationErr := errors.Join(mutationErrors...)
	if mutationErr != nil {
		return nil, fmt.Errorf("mutationEvents: %w", mutationErr)
	}
	return nil, nil
}
