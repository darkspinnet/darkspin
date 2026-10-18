package game

import (
	"cmp"
	"slices"

	"github.com/darkspinnet/darkspin/server/util"
)

type campaignDirectorListenerOwnerKey struct {
	markerSetOrdinal int
	markerID         uint32
}

func campaignDirectorEventHash(event CampaignDirectorEvent) uint32 {
	if event.EventHash != 0 || event.EventName == "" {
		return event.EventHash
	}
	// Legacy server-owned events have a name but no imported hash. Authored
	// records always use their stored ID, including unnamed IDs.
	return util.HashID(event.EventName)
}

func (e *CampaignDirectorSession) instantiateMarkerSetListeners(set CampaignDirectorMarkerSet) {
	markers := slices.Clone(set.Markers)
	for _, trigger := range set.Triggers {
		markers = append(markers, CampaignDirectorMarker{
			Ordinal: trigger.Ordinal, MarkerID: trigger.MarkerID, Name: trigger.Name,
			NounName: trigger.NounName, Position: trigger.Position,
			EventListener: trigger.EventListener,
		})
	}
	slices.SortStableFunc(markers, func(a, b CampaignDirectorMarker) int {
		return cmp.Compare(a.Ordinal, b.Ordinal)
	})
	for _, marker := range markers {
		e.instantiateListenerOwner(set.Ordinal, set.Name, marker)
	}
}

func (e *CampaignDirectorSession) instantiateListenerOwner(
	setOrdinal int, setName string, marker CampaignDirectorMarker,
) {
	key := campaignDirectorListenerOwnerKey{setOrdinal, marker.MarkerID}
	if _, isInstantiated := e.listenerOwners[key]; isInstantiated {
		return
	}
	e.listenerOwners[key] = struct{}{}
	base := CampaignDirectorListenerPublication{
		MarkerSetOrdinal: setOrdinal, MarkerSetName: setName,
		MarkerOrdinal: marker.Ordinal, MarkerID: marker.MarkerID,
		MarkerName: marker.Name, NounName: marker.NounName,
		SpawnKind: marker.SpawnKind, PoolKind: marker.PoolKind,
		IsSpawnKindKnown: marker.IsSpawnKindKnown, Position: marker.Position,
		Rotation: marker.Rotation,
	}
	if marker.EventListener != nil {
		for _, entry := range marker.EventListener.Entries {
			listener := base
			listener.EventHash = entry.EventHash
			listener.EventOrdinal = entry.Ordinal
			listener.NativeCallbackHash = entry.NativeCallbackHash
			if entry.NativeCallbackName != nil {
				listener.NativeCallbackName = *entry.NativeCallbackName
				listener.CallbackName = *entry.NativeCallbackName
			}
			if entry.LuaCallbackName != nil {
				listener.LuaCallbackName = *entry.LuaCallbackName
				if listener.CallbackName == "" {
					listener.CallbackName = listener.LuaCallbackName
				}
			}
			e.listenersByEvent[entry.EventHash] = append(e.listenersByEvent[entry.EventHash], listener)
		}
		return
	}
	// Keep legacy server callback projections until their owner has a structural
	// definition. Trigger entries are never subscriptions.
	for _, event := range marker.Events {
		if event.EventKind != "listener" && event.EventKind != "listener_or_trigger" && event.EventKind != "callback" {
			continue
		}
		listener := base
		eventName := event.EventName
		if eventName == "" {
			eventName = CampaignDirectorCallbackEventName(marker.MarkerID, event.CallbackName)
		}
		if eventName == "" {
			continue
		}
		listener.EventHash = event.EventHash
		if listener.EventHash == 0 {
			listener.EventHash = util.HashID(eventName)
		}
		listener.EventOrdinal = event.Ordinal
		listener.CallbackName = event.CallbackName
		listener.NativeCallbackHash = event.NativeCallbackHash
		if event.LuaCallbackName != nil {
			listener.LuaCallbackName = *event.LuaCallbackName
		}
		e.listenersByEvent[listener.EventHash] = append(e.listenersByEvent[listener.EventHash], listener)
	}
}

// RemoveListenerOwner destroys the selected placement's listener component.
// Native sub_9C3F30 swaps the last subscription into each removed slot and
// rechecks that slot, so remaining entries deliberately do not keep stable order.
// Already prepared publications retain their safe dispatch snapshot.
func (e *CampaignDirectorSession) RemoveListenerOwner(setOrdinal int, markerID uint32) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.listenerOwners, campaignDirectorListenerOwnerKey{setOrdinal, markerID})
	for eventHash, listeners := range e.listenersByEvent {
		for index := 0; index < len(listeners); {
			listener := listeners[index]
			if listener.MarkerSetOrdinal != setOrdinal || listener.MarkerID != markerID {
				index++
				continue
			}
			last := len(listeners) - 1
			listeners[index] = listeners[last]
			listeners[last] = CampaignDirectorListenerPublication{}
			listeners = listeners[:last]
		}
		if len(listeners) == 0 {
			delete(e.listenersByEvent, eventHash)
			continue
		}
		e.listenersByEvent[eventHash] = listeners
	}
}
