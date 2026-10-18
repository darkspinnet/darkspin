package contentsqlite

import (
	"strings"

	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
)

// Lua callback projections can omit the event paired with their trigger slot.
// Retain that authored binding so arena horde listeners are found after the
// tutorial script's activation wait, instead of inventing a callback-only name.
func campaignTriggerEventName(
	definition *contentsqlite.SpawnTriggerDefinition, event contentsqlite.LevelDirectorEvent,
) string {
	if strings.TrimSpace(event.EventName) != "" || definition == nil || definition.TriggerVolume == nil {
		return event.EventName
	}
	trigger := definition.TriggerVolume
	if trigger.Events == nil {
		return event.EventName
	}
	var eventName *string
	switch event.EventSlot {
	case "luaCallbackOnEnter":
		if trigger.LuaCallbackOnEnter != nil && *trigger.LuaCallbackOnEnter == event.CallbackName {
			eventName = trigger.Events.OnEnter
		}
	case "luaCallbackOnExit":
		if trigger.LuaCallbackOnExit != nil && *trigger.LuaCallbackOnExit == event.CallbackName {
			eventName = trigger.Events.OnExit
		}
	}
	if eventName == nil {
		return event.EventName
	}
	return *eventName
}

func campaignSpawnTrigger(definition *contentsqlite.SpawnTriggerDefinition) *game.CampaignSpawnTriggerDefinition {
	if definition == nil {
		return nil
	}
	result := &game.CampaignSpawnTriggerDefinition{DeathEvent: definition.DeathEvent,
		DeathEventHash: definition.DeathEventHash, ChallengeOverride: definition.ChallengeOverride,
		WaveOverride: definition.WaveOverride}
	trigger := definition.TriggerVolume
	if trigger == nil {
		return result
	}
	result.TriggerVolume = campaignTriggerVolume(trigger)
	return result
}

func campaignTeleporter(definition *contentsqlite.TeleporterDefinition) *game.CampaignTeleporterDefinition {
	if definition == nil {
		return nil
	}
	return &game.CampaignTeleporterDefinition{
		DestinationMarkerID:       definition.DestinationMarkerID,
		TriggerVolume:             campaignTriggerVolume(definition.TriggerVolume),
		IsTriggerCreationDeferred: definition.IsTriggerCreationDeferred,
	}
}

func campaignTriggerVolume(trigger *contentsqlite.TriggerVolumeDefinition) *game.CampaignTriggerVolumeDefinition {
	if trigger == nil {
		return nil
	}
	result := &game.CampaignTriggerVolumeDefinition{
		OnEnter: trigger.OnEnter, OnExit: trigger.OnExit, OnStay: trigger.OnStay,
		OnEnterHash: trigger.OnEnterHash, OnExitHash: trigger.OnExitHash, OnStayHash: trigger.OnStayHash,
		IsUsingObjectDimensions: trigger.IsUsingObjectDimensions, IsKinematic: trigger.IsKinematic,
		Shape: trigger.Shape, Offset: game.Vec3{X: trigger.OffsetX, Y: trigger.OffsetY, Z: trigger.OffsetZ},
		TimeToActivate: trigger.TimeToActivate, IsPersistentTimer: trigger.IsPersistentTimer,
		IsTriggerOnceOnly: trigger.IsTriggerOnceOnly, IsTriggerIfNotBeaten: trigger.IsTriggerIfNotBeaten,
		TriggerActivationType: trigger.TriggerActivationType, LuaCallbackOnEnter: trigger.LuaCallbackOnEnter,
		LuaCallbackOnExit: trigger.LuaCallbackOnExit, LuaCallbackOnStay: trigger.LuaCallbackOnStay,
		BoxDimensions: game.Vec3{X: trigger.BoxWidth, Y: trigger.BoxLength, Z: trigger.BoxHeight},
		SphereRadius:  trigger.SphereRadius, CapsuleHeight: trigger.CapsuleHeight,
		CapsuleRadius: trigger.CapsuleRadius, IsServerOnly: trigger.IsServerOnly,
	}
	if trigger.Events != nil {
		result.OnEnterEvent = trigger.Events.OnEnter
		result.OnExitEvent = trigger.Events.OnExit
	}
	return result
}

func campaignEventListener(definition *contentsqlite.EventListenerDefinition) *game.CampaignEventListenerDefinition {
	if definition == nil {
		return nil
	}
	result := &game.CampaignEventListenerDefinition{
		Entries: make([]game.CampaignEventListenerEntry, 0, len(definition.Entries))}
	for _, entry := range definition.Entries {
		result.Entries = append(result.Entries, game.CampaignEventListenerEntry{
			Ordinal: entry.Ordinal, EventHash: entry.EventHash, EventName: entry.EventName,
			NativeCallbackHash: entry.NativeCallbackHash, NativeCallbackName: entry.NativeCallbackName,
			LuaCallbackName: entry.LuaCallbackName})
	}
	return result
}
