package game

import (
	"fmt"
	"time"
)

// CampaignTriggerProgress retains only unfinished authored persistent dwell.
// Active occupancy and actor IDs must be reacquired after loading a checkpoint.
type CampaignTriggerProgress struct {
	MarkerSetOrdinal int
	TriggerOrdinal   int
	TriggerMarkerID  uint32
	EventOrdinal     int
	Elapsed          time.Duration
}

func (e *CampaignDirectorSession) TriggerProgress() []CampaignTriggerProgress {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	progresses := make([]CampaignTriggerProgress, 0)
	for _, event := range e.events {
		if event.trigger.SpawnTrigger == nil || event.trigger.SpawnTrigger.TriggerVolume == nil ||
			!event.trigger.SpawnTrigger.TriggerVolume.IsPersistentTimer {
			continue
		}
		state := e.triggerStates[event.key]
		if state.Elapsed <= 0 || state.IsComplete {
			continue
		}
		progresses = append(progresses, CampaignTriggerProgress{
			MarkerSetOrdinal: event.key.markerSetOrdinal, TriggerOrdinal: event.key.triggerOrdinal,
			TriggerMarkerID: event.trigger.MarkerID, EventOrdinal: event.key.eventOrdinal,
			Elapsed: state.Elapsed,
		})
	}
	return progresses
}

func (e *CampaignDirectorSession) RestoreTriggerProgress(progresses []CampaignTriggerProgress) error {
	if e == nil || len(progresses) == 0 {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	states := make(map[campaignDirectorEventKey]campaignTriggerState)
	for _, progress := range progresses {
		key := campaignDirectorEventKey{
			markerSetOrdinal: progress.MarkerSetOrdinal, triggerOrdinal: progress.TriggerOrdinal,
			eventOrdinal: progress.EventOrdinal,
		}
		if _, isDuplicate := states[key]; isDuplicate {
			return fmt.Errorf("triggerProgress[%d]: duplicate", progress.TriggerMarkerID)
		}
		isMatched := false
		for _, event := range e.events {
			if event.key != key || event.trigger.MarkerID != progress.TriggerMarkerID ||
				event.trigger.SpawnTrigger == nil || event.trigger.SpawnTrigger.TriggerVolume == nil {
				continue
			}
			volume := event.trigger.SpawnTrigger.TriggerVolume
			delay := time.Duration(float64(volume.TimeToActivate) * float64(time.Second))
			isMatched = volume.IsPersistentTimer && progress.Elapsed > 0 && progress.Elapsed < delay
			break
		}
		if !isMatched {
			return fmt.Errorf("triggerProgress[%d]: invalid persistent dwell", progress.TriggerMarkerID)
		}
		states[key] = campaignTriggerState{Elapsed: progress.Elapsed}
	}
	for key, state := range states {
		e.triggerStates[key] = state
	}
	return nil
}
