package boss

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zonecallback "github.com/darkspinnet/darkspin/server/zone/callback"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
)

const GenericCallback = "DirectorTrigger_SpawnBoss"

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
		firstObjectID == 0 {
		return game.CampaignDirectorPublication{}, nil, firstObjectID,
			errors.New("named boss plan: invalid publication")
	}
	var bossListener game.CampaignDirectorListenerPublication
	addListener := make([]game.CampaignDirectorListenerPublication, 0)
	for _, listener := range publication.Listeners {
		if listener.MarkerSetOrdinal != publication.MarkerSetOrdinal ||
			!strings.EqualFold(
				listener.MarkerSetName, publication.MarkerSetName,
			) ||
			listener.MarkerID == 0 || !isFinitePosition(listener.Position) {
			return game.CampaignDirectorPublication{}, nil, firstObjectID,
				errors.New("named boss plan: cross-scoped listener")
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
		return game.CampaignDirectorPublication{}, nil, firstObjectID,
			errors.New("named boss plan: boss anchor unavailable")
	}
	namedBossNoun, isNamedBoss := NamedBossNoun(chainLevelIndex, director.Level)
	if isNamedBoss && IsFinalBossNoun(namedBossNoun) {
		// Shared named-event listeners also contain captain-wave anchors.
		// Destructors own their summons; these anchors are not opening adds.
		addListener = nil
	}
	isZunhObservedFallback := chainLevelIndex == 2 &&
		strings.EqualFold(director.Level, "zelems_3") && len(addListener) == 0
	if isZunhObservedFallback {
		addListener = observedZunhAddListeners(director, bossListener)
	}
	actorCount := 1 + len(addListener)
	if actorCount > int(zoneobject.ProjectileIDStart-firstObjectID) {
		return game.CampaignDirectorPublication{}, nil, firstObjectID,
			errors.New("named boss plan: object IDs exhausted")
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
	agentEntry := CompleteEntries(
		directorPoolEntries(director, "agent"), true,
	)
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
	if len(addListener) != 0 && len(agentEntry) == 0 {
		return game.CampaignDirectorPublication{}, nil, firstObjectID,
			errors.New("named boss plan: add pool unavailable")
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
	profile := zonenpc.ApplyEliteProfile(leader.NPCProfile)
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
	for index, listener := range addListener {
		entryIndex := int(
			(gameID + uint32(index)) % uint32(len(agentEntry)),
		)
		entry := agentEntry[entryIndex]
		plan = append(plan, zonenpc.SpawnPlan{
			ObjectID:      firstObjectID + 1 + uint32(index),
			NounName:      entry.NounName,
			Position:      listener.Position,
			Rotation:      listener.Rotation,
			LocusID:       bossListener.MarkerID,
			Kind:          sim.DirectorLocusBoss,
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

func observedZunhAddListeners(
	director game.CampaignDirector,
	bossListener game.CampaignDirectorListenerPublication,
) []game.CampaignDirectorListenerPublication {
	candidate := make([]game.CampaignDirectorListenerPublication, 0)
	for _, markerSet := range director.MarkerSets {
		if markerSet.Ordinal != bossListener.MarkerSetOrdinal ||
			!strings.EqualFold(markerSet.Name, bossListener.MarkerSetName) {
			continue
		}
		for _, marker := range markerSet.Markers {
			if marker.MarkerID == 0 || !isFinitePosition(marker.Position) ||
				!strings.HasPrefix(strings.ToLower(marker.NounName),
					"spawnpoint_directorhorde.noun") {
				continue
			}
			candidate = append(candidate, game.CampaignDirectorListenerPublication{
				MarkerSetOrdinal: markerSet.Ordinal,
				MarkerSetName:    markerSet.Name, MarkerOrdinal: marker.Ordinal,
				MarkerID: marker.MarkerID, MarkerName: marker.Name,
				NounName: marker.NounName, Position: marker.Position,
				Rotation: marker.Rotation,
			})
		}
	}
	sort.Slice(candidate, func(left int, right int) bool {
		leftDistance := squaredPositionDistance(candidate[left].Position, bossListener.Position)
		rightDistance := squaredPositionDistance(candidate[right].Position, bossListener.Position)
		if leftDistance == rightDistance {
			return candidate[left].MarkerID < candidate[right].MarkerID
		}
		return leftDistance < rightDistance
	})
	if len(candidate) >= 2 {
		return candidate[:2]
	}
	for len(candidate) < 2 {
		index := len(candidate)
		offset := float32(3)
		if index != 0 {
			offset = -offset
		}
		candidate = append(candidate, game.CampaignDirectorListenerPublication{
			MarkerSetOrdinal: bossListener.MarkerSetOrdinal,
			MarkerSetName:    bossListener.MarkerSetName,
			MarkerOrdinal:    bossListener.MarkerOrdinal,
			MarkerID:         bossListener.MarkerID,
			MarkerName:       bossListener.MarkerName,
			NounName:         "SpawnPoint_DirectorHorde.Noun",
			Position: game.Vec3{
				X: bossListener.Position.X + offset,
				Y: bossListener.Position.Y,
				Z: bossListener.Position.Z,
			},
		})
	}
	return candidate
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
		for _, marker := range markerSet.Markers {
			for _, event := range marker.Events {
				if !IsNamedCallback(event.CallbackName) {
					continue
				}
				eventName := namedMarkerEventName(marker, event)
				if eventName == "" {
					continue
				}
				listener := namedEventListeners(markerSet, eventName)
				return game.CampaignDirectorNamedEventPublication{
					PublicationID:    uint64(marker.MarkerID),
					MarkerSetOrdinal: markerSet.Ordinal,
					MarkerSetName:    markerSet.Name,
					SourceObjectID:   marker.MarkerID,
					EventName:        eventName,
					Listeners:        listener,
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
	for _, markerSet := range director.MarkerSets {
		for _, marker := range markerSet.Markers {
			for _, event := range marker.Events {
				if !IsNamedCallback(event.CallbackName) {
					continue
				}
				eventName := namedMarkerEventName(marker, event)
				if eventName == "" {
					continue
				}
				listeners := namedEventListeners(markerSet, eventName)
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
						PublicationID:    uint64(marker.MarkerID),
						MarkerSetOrdinal: markerSet.Ordinal,
						MarkerSetName:    markerSet.Name,
						SourceObjectID:   marker.MarkerID,
						EventName:        eventName,
						Listeners:        listeners,
					}
				}
			}
		}
	}
	if nearestPublication.PublicationID == 0 {
		return game.CampaignDirectorNamedEventPublication{},
			fmt.Errorf("near boss publication: level %q unavailable", director.Level)
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
	listener := make([]game.CampaignDirectorListenerPublication, 0)
	for _, candidate := range markerSet.Markers {
		for _, candidateEvent := range candidate.Events {
			candidateEventName := namedMarkerEventName(candidate, candidateEvent)
			if !strings.EqualFold(candidateEventName, eventName) {
				continue
			}
			listener = append(listener,
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
	return listener
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

func isFinitePosition(position game.Vec3) bool {
	return !math.IsNaN(float64(position.X)) &&
		!math.IsNaN(float64(position.Y)) &&
		!math.IsNaN(float64(position.Z)) &&
		!math.IsInf(float64(position.X), 0) &&
		!math.IsInf(float64(position.Y), 0) &&
		!math.IsInf(float64(position.Z), 0)
}
