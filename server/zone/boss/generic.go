package boss

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zonecallback "github.com/darkspinnet/darkspin/server/zone/callback"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
	zonepopulation "github.com/darkspinnet/darkspin/server/zone/population"
)

const GenericCallback = "DirectorTrigger_SpawnBoss"

// ErrNoNamedBossNearby is the expected proximity miss; malformed nearby
// trigger or listener data returns a separate diagnostic error.
var ErrNoNamedBossNearby = errors.New("no named boss nearby")

const NamedBossArenaRadius = float32(18)

func IsNamedCallback(callbackName string) bool {
	return callbackName == GenericCallback ||
		callbackName == zonecallback.CatalystUnlock ||
		callbackName == zonecallback.OverdriveUnlock
}

func PlanNamedEncounter(
	director game.CampaignDirector,
	publication game.CampaignDirectorNamedEventPublication,
	firstObjectID uint32, gameID uint32, chainLevelIndex uint32,
) (game.CampaignDirectorPublication, []zonenpc.SpawnPlan, uint32, error) {
	if publication.PublicationID == 0 || publication.MarkerSetOrdinal < 0 ||
		publication.MarkerSetName == "" || publication.EventName == "" ||
		firstObjectID == 0 || firstObjectID >= zoneobject.ProjectileIDStart {
		return game.CampaignDirectorPublication{}, nil, firstObjectID,
			errors.New("named boss plan: invalid publication")
	}
	var bossListener game.CampaignDirectorListenerPublication
	addListener := make([]game.CampaignDirectorListenerPublication, 0)
	for _, listener := range publication.Listeners {
		if listener.MarkerID == 0 || !isFinitePosition(listener.Position) {
			return game.CampaignDirectorPublication{}, nil, firstObjectID,
				errors.New("named boss plan: invalid listener")
		}
		switch {
		case IsNamedCallback(listener.CallbackName):
			if bossListener.MarkerID != 0 ||
				!strings.EqualFold(
					listener.NounName, "SpawnPoint_DirectorBoss.Noun",
				) {
				return game.CampaignDirectorPublication{}, nil, firstObjectID,
					errors.New("named boss plan: invalid boss anchor")
			}
			bossListener = listener
		case listener.CallbackName == "HordeSpawner_Register":
			if !strings.EqualFold(
				listener.NounName, "SpawnPoint_DirectorHorde.Noun",
			) {
				return game.CampaignDirectorPublication{}, nil, firstObjectID,
					errors.New("named boss plan: invalid add anchor")
			}
			addListener = append(addListener, listener)
		}
	}
	if bossListener.MarkerID == 0 {
		triggerAnchor, anchorErr := namedBossTriggerAnchor(director, publication)
		if anchorErr != nil {
			return game.CampaignDirectorPublication{}, nil, firstObjectID,
				fmt.Errorf("named boss trigger anchor: %w", anchorErr)
		}
		bossListener = triggerAnchor
	}
	if bossListener.MarkerID == 0 {
		return game.CampaignDirectorPublication{}, nil, firstObjectID,
			errors.New("named boss plan: boss anchor unavailable")
	}
	namedBossNoun, isNamedBoss := NamedBossNoun(chainLevelIndex, director.Level)
	if isNamedBoss && IsFinalBossNoun(namedBossNoun) {
		// Shared named-event listeners also contain captain-wave anchors.
		// Destructors own their summons; these anchors are not opening adds.
		addListener = nil
	} else if len(addListener) == 0 {
		return game.CampaignDirectorPublication{}, nil, firstObjectID,
			fmt.Errorf("bossAddAnchors: level %q event %q has no horde listeners",
				director.Level, publication.EventName)
	}
	leaderEntry := CompleteEntries(
		directorPoolEntries(director, "captain"), false,
	)
	if isNamedBoss {
		leaderEntry = NamedBossEntries(director, chainLevelIndex)
	} else if len(leaderEntry) == 0 {
		leaderEntry = CompleteEntries(
			directorPoolEntries(director, "special"), false,
		)
	}
	addEntries := bossAddEntries(director)
	if len(leaderEntry) == 0 {
		if isNamedBoss {
			return game.CampaignDirectorPublication{}, nil, firstObjectID,
				fmt.Errorf(
					"named boss plan: complete %q unavailable",
					namedBossNoun,
				)
		}
		return game.CampaignDirectorPublication{}, nil, firstObjectID,
			errors.New("named boss plan: candidate pool unavailable")
	}
	if len(addListener) != 0 && len(addEntries) == 0 {
		// Horde anchors consume the selected minion roster, not an obligatory
		// agent pool. Never silently skip the wave when its roster is invalid.
		return game.CampaignDirectorPublication{}, nil, firstObjectID,
			fmt.Errorf("bossAddRoster: level %q has %d anchors but no eligible horde minions",
				director.Level, len(addListener))
	}
	expandedListeners := make([]game.CampaignDirectorListenerPublication, 0)
	for _, listener := range addListener {
		positions := zonepopulation.GroupPositions([]game.Vec3{listener.Position}, int(director.BossSpawnRules.Resolved().OpeningAddsPerAnchor))
		for _, position := range positions {
			copyListener := listener
			copyListener.Position = position
			expandedListeners = append(expandedListeners, copyListener)
		}
	}
	addListener = expandedListeners
	actorCount := 1 + len(addListener)
	if actorCount > int(zoneobject.ProjectileIDStart-firstObjectID) {
		return game.CampaignDirectorPublication{}, nil, firstObjectID,
			errors.New("named boss plan: object IDs exhausted")
	}
	leader, err := SelectNamedLeader(
		director, leaderEntry, chainLevelIndex, gameID, bossListener.MarkerID,
	)
	if err != nil {
		return game.CampaignDirectorPublication{}, nil, firstObjectID,
			fmt.Errorf("named boss leader: %w", err)
	}
	bossIdentity, isIdentityFound := namedBossIdentity(director, leader.NounName)
	if !isIdentityFound {
		return game.CampaignDirectorPublication{}, nil, firstObjectID,
			fmt.Errorf(
				"named boss plan: identity unavailable for %q",
				leader.NounName,
			)
	}
	isCaptain := !IsFinalBossNoun(leader.NounName)
	profile := leader.NPCProfile
	if isCaptain {
		profile = zonenpc.ApplyCaptainProfile(profile, bossIdentity)
	} else {
		profile = zonenpc.ApplyEliteProfile(profile)
	}
	plan := []zonenpc.SpawnPlan{{
		ObjectID:      firstObjectID,
		NounName:      leader.NounName,
		Position:      bossListener.Position,
		Rotation:      bossListener.Rotation,
		LocusID:       bossListener.MarkerID,
		Kind:          sim.DirectorLocusBoss,
		IsCaptain:     isCaptain,
		IsBoss:        true,
		MarkerSetName: publication.MarkerSetName,
		NPCProfile:    profile,
		BossIdentity:  bossIdentity,
	}}
	// Retain the local one-add-per-anchor policy until native wave budgets are
	// recovered; the authored listeners determine placement, not a fake ring.
	random := initialAddRandom(director, gameID, bossListener.MarkerID, 1)
	for index, listener := range addListener {
		entryIndex, choiceErr := random.Index(uint32(len(addEntries)))
		if choiceErr != nil {
			return game.CampaignDirectorPublication{}, nil, firstObjectID,
				fmt.Errorf("bossAddChoice[%d]: %w", index, choiceErr)
		}
		entry := addEntries[entryIndex]
		plan = append(plan, zonenpc.SpawnPlan{
			ObjectID:      firstObjectID + 1 + uint32(index),
			NounName:      entry.NounName,
			Position:      listener.Position,
			Rotation:      listener.Rotation,
			LocusID:       bossListener.MarkerID,
			Kind:          sim.DirectorLocusBoss,
			Introduction:  zonenpc.SpawnIntroductionFloorWarp,
			MarkerSetName: publication.MarkerSetName,
			NPCProfile:    entry.NPCProfile,
		})
	}
	triggerPublication := game.CampaignDirectorPublication{
		MarkerSetOrdinal: publication.MarkerSetOrdinal,
		MarkerSetName:    publication.MarkerSetName,
		TriggerMarkerID:  bossListener.MarkerID,
		TriggerName:      bossListener.MarkerName,
		EventName:        publication.EventName,
		CallbackName:     bossListener.CallbackName,
		Listeners: append(
			[]game.CampaignDirectorListenerPublication(nil),
			publication.Listeners...,
		),
	}
	return triggerPublication, plan, firstObjectID + uint32(len(plan)), nil
}

func squaredPositionDistance(left game.Vec3, right game.Vec3) float32 {
	deltaX := left.X - right.X
	deltaY := left.Y - right.Y
	deltaZ := left.Z - right.Z
	return deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ
}

func DeveloperPublication(
	director game.CampaignDirector,
) (game.CampaignDirectorNamedEventPublication, error) {
	for _, markerSet := range director.MarkerSets {
		for _, trigger := range markerSet.Triggers {
			for _, event := range trigger.Events {
				if !IsNamedCallback(event.CallbackName) {
					continue
				}
				eventName := namedTriggerEventName(trigger, event)
				if eventName == "" {
					continue
				}
				listeners := namedEventListeners(markerSet, eventName)
				triggerAnchor, anchorErr := bossTriggerAnchor(markerSet, trigger, event)
				if anchorErr != nil {
					return game.CampaignDirectorNamedEventPublication{},
						fmt.Errorf("generic boss trigger anchor: %w", anchorErr)
				}
				if triggerAnchor.MarkerID != 0 {
					listeners = append(listeners, triggerAnchor)
				}
				if len(listeners) == 0 {
					return game.CampaignDirectorNamedEventPublication{},
						fmt.Errorf("generic boss listeners: level %q set=%q trigger=%d event=%q empty",
							director.Level, markerSet.Name, trigger.MarkerID, eventName)
				}
				return game.CampaignDirectorNamedEventPublication{
					PublicationID:    uint64(trigger.MarkerID),
					MarkerSetOrdinal: markerSet.Ordinal,
					MarkerSetName:    markerSet.Name,
					SourceObjectID:   trigger.MarkerID,
					EventName:        eventName,
					Listeners:        listeners,
				}, nil
			}
		}
	}
	return game.CampaignDirectorNamedEventPublication{},
		fmt.Errorf("generic boss publication: level %q unavailable", director.Level)
}

func DeveloperPublicationNearPosition(
	director game.CampaignDirector, position game.Vec3, maximumDistance float32,
) (game.CampaignDirectorNamedEventPublication, error) {
	if maximumDistance <= 0 || !isFinitePosition(position) {
		return game.CampaignDirectorNamedEventPublication{},
			errors.New("near boss publication invalid")
	}
	maximumDistanceSquared := maximumDistance * maximumDistance
	nearestDistanceSquared := float32(math.MaxFloat32)
	nearestPublication := game.CampaignDirectorNamedEventPublication{}
	isBossAnchorNearby := false
	isNamedTriggerNearby := false
	nearMarkerSetName := ""
	nearTriggerMarkerID := uint32(0)
	nearEventName := ""
	for _, markerSet := range director.MarkerSets {
		for _, marker := range markerSet.Markers {
			if strings.EqualFold(marker.NounName, "SpawnPoint_DirectorBoss.Noun") &&
				squaredPositionDistance(position, marker.Position) <= maximumDistanceSquared {
				isBossAnchorNearby = true
				nearMarkerSetName = markerSet.Name
			}
		}
		for _, trigger := range markerSet.Triggers {
			for _, event := range trigger.Events {
				if !IsNamedCallback(event.CallbackName) {
					continue
				}
				if squaredPositionDistance(position, trigger.Position) <= maximumDistanceSquared {
					isNamedTriggerNearby = true
					nearMarkerSetName = markerSet.Name
					nearTriggerMarkerID = trigger.MarkerID
					nearEventName = namedTriggerEventName(trigger, event)
				}
				eventName := namedTriggerEventName(trigger, event)
				if eventName == "" {
					continue
				}
				listeners := namedEventListeners(markerSet, eventName)
				triggerAnchor, anchorErr := bossTriggerAnchor(markerSet, trigger, event)
				if anchorErr != nil {
					return game.CampaignDirectorNamedEventPublication{},
						fmt.Errorf("near boss trigger anchor: %w", anchorErr)
				}
				if triggerAnchor.MarkerID != 0 {
					listeners = append(listeners, triggerAnchor)
				}
				for _, listener := range listeners {
					if !IsNamedCallback(listener.CallbackName) ||
						!strings.EqualFold(
							listener.NounName, "SpawnPoint_DirectorBoss.Noun",
						) {
						continue
					}
					distanceSquared := squaredPositionDistance(position, listener.Position)
					if distanceSquared > maximumDistanceSquared ||
						distanceSquared >= nearestDistanceSquared {
						continue
					}
					nearestDistanceSquared = distanceSquared
					nearestPublication = game.CampaignDirectorNamedEventPublication{
						PublicationID:    uint64(trigger.MarkerID),
						MarkerSetOrdinal: markerSet.Ordinal,
						MarkerSetName:    markerSet.Name,
						SourceObjectID:   trigger.MarkerID,
						EventName:        eventName,
						Listeners:        listeners,
					}
				}
			}
		}
	}
	if nearestPublication.PublicationID == 0 {
		if isBossAnchorNearby || isNamedTriggerNearby {
			return game.CampaignDirectorNamedEventPublication{},
				fmt.Errorf("near boss listener: level %q set=%q trigger=%d event=%q anchor_near=%t trigger_near=%t",
					director.Level, nearMarkerSetName, nearTriggerMarkerID,
					nearEventName, isBossAnchorNearby, isNamedTriggerNearby)
		}
		return game.CampaignDirectorNamedEventPublication{},
			fmt.Errorf("near boss publication: level %q: %w", director.Level, ErrNoNamedBossNearby)
	}
	return nearestPublication, nil
}

func IsFinalAuthoredHordeCompletion(
	director game.CampaignDirector, completion zonehorde.Completion,
) bool {
	if director.Level == "" ||
		strings.EqualFold(director.Level, game.InitialChainLevel) ||
		completion.MarkerSetName == "" ||
		completion.EventName != "horde complete" {
		return false
	}
	finalOrdinal := -1
	finalMarkerSetName := ""
	completionPosition := game.Vec3{}
	isCompletionPositionFound := false
	for _, markerSet := range director.MarkerSets {
		for _, trigger := range markerSet.Triggers {
			if strings.EqualFold(markerSet.Name, completion.MarkerSetName) &&
				trigger.MarkerID == completion.TriggerMarkerID &&
				isFinitePosition(trigger.Position) {
				completionPosition = trigger.Position
				isCompletionPositionFound = true
			}
			for _, event := range trigger.Events {
				if event.CallbackName != "HordeTrigger_OnEnterPlayer" ||
					event.EventName != "horde triggered" ||
					event.TriggerRadius <= 0 {
					continue
				}
				if markerSet.Ordinal > finalOrdinal {
					finalOrdinal = markerSet.Ordinal
					finalMarkerSetName = markerSet.Name
				}
			}
		}
	}
	if finalOrdinal < 0 ||
		!strings.EqualFold(finalMarkerSetName, completion.MarkerSetName) ||
		!isCompletionPositionFound {
		return false
	}
	bossPublication, err := DeveloperPublication(director)
	if err != nil {
		return false
	}
	maximumDistanceSquared := NamedBossArenaRadius * NamedBossArenaRadius
	for _, listener := range bossPublication.Listeners {
		if !IsNamedCallback(listener.CallbackName) ||
			!strings.EqualFold(listener.NounName, "SpawnPoint_DirectorBoss.Noun") {
			continue
		}
		return squaredPositionDistance(completionPosition, listener.Position) <=
			maximumDistanceSquared
	}
	return false
}

func directorPoolEntries(
	director game.CampaignDirector, configKind string,
) []game.CampaignDirectorEntry {
	for _, pool := range director.Pools {
		if strings.EqualFold(pool.ConfigKind, configKind) {
			return pool.Entries
		}
	}
	return nil
}

func namedEventListeners(
	markerSet game.CampaignDirectorMarkerSet, eventName string,
) []game.CampaignDirectorListenerPublication {
	listeners := make([]game.CampaignDirectorListenerPublication, 0)
	eventHash := util.HashID(eventName)
	for _, candidate := range markerSet.Markers {
		if candidate.EventListener != nil {
			// Use the same structural subscriptions as the live director. The
			// legacy projection can split authored event names into callbacks.
			for _, entry := range candidate.EventListener.Entries {
				if entry.EventHash != eventHash {
					continue
				}
				listener := game.CampaignDirectorListenerPublication{
					MarkerSetOrdinal: markerSet.Ordinal, MarkerSetName: markerSet.Name,
					MarkerOrdinal: candidate.Ordinal, MarkerID: candidate.MarkerID,
					MarkerName: candidate.Name, NounName: candidate.NounName,
					SpawnKind: candidate.SpawnKind, PoolKind: candidate.PoolKind,
					IsSpawnKindKnown: candidate.IsSpawnKindKnown,
					Position:         candidate.Position, Rotation: candidate.Rotation,
					EventOrdinal: entry.Ordinal, EventHash: entry.EventHash,
					NativeCallbackHash: entry.NativeCallbackHash,
				}
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
				listeners = append(listeners, listener)
			}
			continue
		}
		for _, candidateEvent := range candidate.Events {
			candidateEventName := namedMarkerEventName(candidate, candidateEvent)
			if !strings.EqualFold(candidateEventName, eventName) {
				continue
			}
			listeners = append(listeners,
				game.CampaignDirectorListenerPublication{
					MarkerSetOrdinal: markerSet.Ordinal,
					MarkerSetName:    markerSet.Name,
					MarkerOrdinal:    candidate.Ordinal,
					MarkerID:         candidate.MarkerID,
					MarkerName:       candidate.Name,
					NounName:         candidate.NounName,
					SpawnKind:        candidate.SpawnKind,
					PoolKind:         candidate.PoolKind,
					IsSpawnKindKnown: candidate.IsSpawnKindKnown,
					Position:         candidate.Position,
					Rotation:         candidate.Rotation,
					EventOrdinal:     candidateEvent.Ordinal,
					CallbackName:     candidateEvent.CallbackName,
				},
			)
		}
	}
	return listeners
}

// A boss spawn point can own the trigger itself. The director stores that
// placement in Triggers, while live named-event subscriptions contain only
// separate listener components such as the horde add anchors.
func bossTriggerAnchor(
	markerSet game.CampaignDirectorMarkerSet,
	trigger game.CampaignDirectorTrigger,
	event game.CampaignDirectorEvent,
) (game.CampaignDirectorListenerPublication, error) {
	if !strings.EqualFold(trigger.NounName, "SpawnPoint_DirectorBoss.Noun") ||
		!IsNamedCallback(event.CallbackName) {
		return game.CampaignDirectorListenerPublication{}, nil
	}
	for _, definition := range markerSet.Definitions {
		if definition.MarkerID != trigger.MarkerID {
			continue
		}
		return game.CampaignDirectorListenerPublication{
			MarkerSetOrdinal: markerSet.Ordinal, MarkerSetName: markerSet.Name,
			MarkerOrdinal: trigger.Ordinal, MarkerID: trigger.MarkerID,
			MarkerName: trigger.Name, NounName: trigger.NounName,
			Position: trigger.Position, Rotation: definition.Rotation,
			EventOrdinal: event.Ordinal, CallbackName: event.CallbackName,
		}, nil
	}
	return game.CampaignDirectorListenerPublication{}, fmt.Errorf(
		"level marker definition: set=%q trigger=%d missing",
		markerSet.Name, trigger.MarkerID,
	)
}

func namedBossTriggerAnchor(
	director game.CampaignDirector,
	publication game.CampaignDirectorNamedEventPublication,
) (game.CampaignDirectorListenerPublication, error) {
	for _, markerSet := range director.MarkerSets {
		if markerSet.Ordinal != publication.MarkerSetOrdinal ||
			!strings.EqualFold(markerSet.Name, publication.MarkerSetName) {
			continue
		}
		for _, trigger := range markerSet.Triggers {
			if trigger.MarkerID != publication.SourceObjectID {
				continue
			}
			for _, event := range trigger.Events {
				// A trigger's native event and Lua callback are separate slots
				// with the same authored event name. The event-only slot is not
				// the boss operation; keep looking for its named callback.
				if !IsNamedCallback(event.CallbackName) {
					continue
				}
				if !strings.EqualFold(
					namedTriggerEventName(trigger, event), publication.EventName,
				) {
					continue
				}
				anchor, err := bossTriggerAnchor(markerSet, trigger, event)
				if err != nil {
					return game.CampaignDirectorListenerPublication{},
						fmt.Errorf("bossAnchor: %w", err)
				}
				return anchor, nil
			}
		}
	}
	return game.CampaignDirectorListenerPublication{}, nil
}

func namedMarkerEventName(
	marker game.CampaignDirectorMarker, event game.CampaignDirectorEvent,
) string {
	if strings.TrimSpace(event.EventName) != "" {
		return event.EventName
	}
	return game.CampaignDirectorCallbackEventName(
		marker.MarkerID, event.CallbackName,
	)
}

func namedTriggerEventName(
	trigger game.CampaignDirectorTrigger, event game.CampaignDirectorEvent,
) string {
	if strings.TrimSpace(event.EventName) != "" {
		return event.EventName
	}
	return game.CampaignDirectorCallbackEventName(trigger.MarkerID, event.CallbackName)
}

func isFinitePosition(position game.Vec3) bool {
	return !math.IsNaN(float64(position.X)) &&
		!math.IsNaN(float64(position.Y)) &&
		!math.IsNaN(float64(position.Z)) &&
		!math.IsInf(float64(position.X), 0) &&
		!math.IsInf(float64(position.Y), 0) &&
		!math.IsInf(float64(position.Z), 0)
}
