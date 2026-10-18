package game

import (
	"errors"
	"fmt"
	"time"
)

// CampaignTriggerParticipant describes a currently participating player slot,
// not an invited account or a contiguous index into the party.
type CampaignTriggerParticipant struct {
	Slot          uint16
	ObjectID      uint32
	Position      Vec3
	IsLevelBeaten bool
}

type CampaignTriggerRequest struct {
	ActorObjectID uint32
	Previous      Vec3
	Current       Vec3
	Elapsed       time.Duration
	Participants  []CampaignTriggerParticipant
}

type campaignTriggerState struct {
	ActorObjectID uint32
	Elapsed       time.Duration
	SampledAt     time.Duration
	IsOccupied    bool
	IsComplete    bool
}

// SuspendTriggers prevents time with no connected players counting as dwell.
// Retained partial progress follows only the authored persistent-timer flag.
func (e *CampaignDirectorSession) SuspendTriggers() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, event := range e.events {
		if event.trigger.SpawnTrigger == nil || event.trigger.SpawnTrigger.TriggerVolume == nil {
			continue
		}
		state := e.triggerStates[event.key]
		if !event.trigger.SpawnTrigger.TriggerVolume.IsPersistentTimer || state.IsComplete {
			state.Elapsed = 0
		}
		state.IsOccupied = false
		state.IsComplete = false
		state.ActorObjectID = 0
		e.triggerStates[event.key] = state
	}
}

// AdvanceParticipants applies the recovered A1B530/A1B5D0 party gates and
// A1BAF0/A1B700 dwell timer before any encounter or tutorial operation. Time is
// absolute zone simulation time, so polling more peers cannot accelerate it.
func (e *CampaignDirectorSession) AdvanceParticipants(
	req CampaignTriggerRequest,
) ([]CampaignDirectorPublication, error) {
	if e == nil || req.Elapsed < 0 || !isFiniteCampaignPosition(req.Previous) ||
		!isFiniteCampaignPosition(req.Current) {
		return nil, errors.New("invalid trigger request")
	}
	slots := make(map[uint16]bool, len(req.Participants))
	isRequestActorPresent := false
	for _, participant := range req.Participants {
		if participant.ObjectID == 0 || slots[participant.Slot] ||
			!isFiniteCampaignPosition(participant.Position) {
			return nil, errors.New("invalid trigger participant")
		}
		slots[participant.Slot] = true
		isRequestActorPresent = isRequestActorPresent || participant.ObjectID == req.ActorObjectID
	}
	publications := make([]CampaignDirectorPublication, 0)
	if isRequestActorPresent {
		var err error
		publications, err = e.advanceMovement(req.Previous, req.Current, true)
		if err != nil {
			return nil, fmt.Errorf("triggerLegacy: %w", err)
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, event := range e.events {
		if event.trigger.SpawnTrigger == nil || event.trigger.SpawnTrigger.TriggerVolume == nil {
			continue
		}
		volume := event.trigger.SpawnTrigger.TriggerVolume
		if e.onceOnlyEventPolicies[event.key] {
			if _, isFired := e.firedPublications[event.key]; isFired {
				continue
			}
		}
		state := e.triggerStates[event.key]
		if req.Elapsed < state.SampledAt {
			continue
		}
		insideParticipants := make([]CampaignTriggerParticipant, 0, len(req.Participants))
		isActorPresent := false
		for _, participant := range req.Participants {
			if participant.ObjectID == state.ActorObjectID {
				isActorPresent = true
			}
			if volume.IsTriggerIfNotBeaten && participant.IsLevelBeaten {
				continue
			}
			isInside := campaignTriggerContains(event.trigger, participant.Position)
			if !isInside && volume.TimeToActivate == 0 &&
				volume.TriggerActivationType == uint32(TriggerAnyObject) &&
				participant.ObjectID == req.ActorObjectID {
				isInside = campaignTriggerCrossed(event.trigger, req.Previous, req.Current)
			}
			if isInside {
				insideParticipants = append(insideParticipants, participant)
			}
		}
		isOccupied := len(insideParticipants) > 0
		if volume.TriggerActivationType == uint32(TriggerAllPlayers) {
			isOccupied = isOccupied && len(insideParticipants) == len(req.Participants)
		}
		// All authored campaign spawn triggers use first-player or all-player
		// admission. Other trigger families retain their existing owner.
		if volume.TriggerActivationType != uint32(TriggerAnyObject) &&
			volume.TriggerActivationType != uint32(TriggerAllPlayers) {
			continue
		}
		if !isActorPresent && len(insideParticipants) > 0 {
			replacementID := insideParticipants[0].ObjectID
			if state.ActorObjectID == 0 {
				for _, participant := range insideParticipants {
					if participant.ObjectID == req.ActorObjectID {
						replacementID = req.ActorObjectID
						break
					}
				}
			}
			state.ActorObjectID = replacementID
			isActorPresent = true
		}
		pending, isPending := e.pendingPublications[event.key]
		if isPending {
			// Dwell already completed. Retry acceptance without restarting it;
			// replace a departed actor with a live participating occupant.
			pending.ActorObjectID = state.ActorObjectID
			e.pendingPublications[event.key] = pending
			if isActorPresent && pending.ActorObjectID == req.ActorObjectID {
				publications = append(publications, cloneCampaignDirectorPublication(pending))
			}
			state.SampledAt = req.Elapsed
			e.triggerStates[event.key] = state
			continue
		}
		if !isOccupied {
			if !volume.IsPersistentTimer || state.IsComplete {
				state.Elapsed = 0
			}
			state.IsOccupied = false
			state.IsComplete = false
			state.ActorObjectID = 0
			state.SampledAt = req.Elapsed
			e.triggerStates[event.key] = state
			continue
		}
		if state.IsOccupied && !state.IsComplete {
			state.Elapsed += req.Elapsed - state.SampledAt
		}
		state.SampledAt = req.Elapsed
		state.IsOccupied = true
		delay := time.Duration(float64(volume.TimeToActivate) * float64(time.Second))
		if !state.IsComplete && state.Elapsed >= delay {
			state.IsComplete = true
			publication := CampaignDirectorPublication{
				ActorObjectID: state.ActorObjectID, IsDwellComplete: true,
				MarkerSetOrdinal: event.key.markerSetOrdinal, MarkerSetName: event.markerSetName,
				TriggerOrdinal: event.key.triggerOrdinal, TriggerMarkerID: event.trigger.MarkerID,
				TriggerName: event.trigger.Name, EventOrdinal: event.key.eventOrdinal,
				EventName: event.event.EventName, CallbackName: event.event.CallbackName,
				EventHash: campaignDirectorEventHash(event.event),
				Listeners: append([]CampaignDirectorListenerPublication(nil),
					e.listenersByEvent[campaignDirectorEventHash(event.event)]...),
			}
			e.pendingPublications[event.key] = publication
			if publication.ActorObjectID == req.ActorObjectID {
				publications = append(publications, cloneCampaignDirectorPublication(publication))
			}
		}
		e.triggerStates[event.key] = state
	}
	return publications, nil
}
