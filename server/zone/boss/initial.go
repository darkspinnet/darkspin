package boss

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
	zonepopulation "github.com/darkspinnet/darkspin/server/zone/population"
)

const (
	InitialMarkerSetName   = "zelems_1_design_spawners.Markerset"
	InitialTriggerCallback = "nTutorial_SoloSupportUnlockClient.main"
	InitialArmingDelay     = 2 * time.Second
	InitialSecondWaveDelay = 2 * time.Second
)

var ErrSecondHordeIncomplete = errors.New("boss order: second horde incomplete")

func SelectInitialLeader(
	entries []game.CampaignDirectorEntry, chainLevelIndex uint32,
) (game.CampaignDirectorEntry, error) {
	nounName, isFound := NamedBossNoun(chainLevelIndex, game.InitialChainLevel)
	if !isFound {
		return game.CampaignDirectorEntry{}, errors.New("boss leader: chain slot invalid")
	}
	var selected game.CampaignDirectorEntry
	for _, entry := range entries {
		if !strings.EqualFold(
			entry.NounName, nounName,
		) {
			continue
		}
		if selected.NounName != "" {
			return game.CampaignDirectorEntry{},
				fmt.Errorf("boss leader: duplicate %q", nounName)
		}
		selected = entry
	}
	if selected.NounName == "" || !selected.NPCProfile.IsKnown {
		return game.CampaignDirectorEntry{},
			fmt.Errorf("boss leader: %q unavailable", nounName)
	}
	return selected, nil
}

func PlanInitialEncounter(
	director game.CampaignDirector,
	publication game.CampaignDirectorPublication,
	firstObjectID uint32, gameID uint32, chainLevelIndex uint32,
) ([]zonenpc.SpawnPlan, uint32, error) {
	if !strings.EqualFold(publication.MarkerSetName, InitialMarkerSetName) ||
		publication.CallbackName != InitialTriggerCallback {
		return nil, firstObjectID, nil
	}
	if publication.TriggerMarkerID == 0 || firstObjectID == 0 ||
		firstObjectID > zoneobject.ProjectileIDStart-5 {
		return nil, firstObjectID, errors.New("boss plan: invalid publication")
	}
	bossMarker, addMarker, err := initialMarkers(director, publication)
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("bossMarkers: %w", err)
	}
	specialEntry := zonepopulation.PoolEntries(director, "special")
	agentEntry := eligibleInitialAgents(director)
	if len(specialEntry) == 0 || len(agentEntry) == 0 {
		return nil, firstObjectID, errors.New("boss plan: empty pool")
	}
	leaderEntry, err := SelectInitialLeader(specialEntry, chainLevelIndex)
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("boss plan leader: %w", err)
	}
	bossIdentity, isBossIdentityFound := namedBossIdentity(
		director, leaderEntry.NounName,
	)
	if !isBossIdentityFound {
		return nil, firstObjectID, errors.New("boss plan identity: missing")
	}
	leaderEntry.NPCProfile = zonenpc.ApplyEliteProfile(leaderEntry.NPCProfile)
	plans := []zonenpc.SpawnPlan{{
		ObjectID: firstObjectID, NounName: leaderEntry.NounName,
		Position: bossMarker.Position, LocusID: bossMarker.MarkerID,
		Rotation: bossMarker.Rotation,
		Kind:     sim.DirectorLocusBoss, IsCaptain: true, IsBoss: true,
		MarkerSetName: publication.MarkerSetName,
		NPCProfile:    leaderEntry.NPCProfile,
		BossIdentity:  bossIdentity,
	}}
	startIndex := int(
		(gameID ^ bossMarker.MarkerID) % uint32(len(agentEntry)),
	)
	for index, marker := range addMarker {
		entry := agentEntry[(startIndex+index)%len(agentEntry)]
		plans = append(plans, zonenpc.SpawnPlan{
			ObjectID: firstObjectID + 1 + uint32(index),
			NounName: entry.NounName, Position: marker.Position,
			Rotation:      marker.Rotation,
			LocusID:       publication.TriggerMarkerID,
			Kind:          sim.DirectorLocusBoss,
			MarkerSetName: publication.MarkerSetName,
			NPCProfile:    entry.NPCProfile,
		})
	}
	return plans, firstObjectID + uint32(len(plans)), nil
}

func PlanInitialSecondWave(
	director game.CampaignDirector,
	publication game.CampaignDirectorPublication,
	firstObjectID uint32, gameID uint32,
) ([]zonenpc.SpawnPlan, uint32, error) {
	if !strings.EqualFold(publication.MarkerSetName, InitialMarkerSetName) ||
		publication.CallbackName != InitialTriggerCallback ||
		publication.TriggerMarkerID == 0 || firstObjectID == 0 ||
		firstObjectID > zoneobject.ProjectileIDStart-2 {
		return nil, firstObjectID, errors.New("boss second plan: invalid")
	}
	addMarker, err := initialAddMarkers(director, publication)
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("bossSecondMarkers: %w", err)
	}
	agentEntry := eligibleInitialAgents(director)
	if len(agentEntry) == 0 {
		return nil, firstObjectID, errors.New("boss second plan: empty pool")
	}
	startIndex := int(
		(gameID ^ publication.TriggerMarkerID ^ 0x206) %
			uint32(len(agentEntry)),
	)
	plans := make([]zonenpc.SpawnPlan, 0, 2)
	for index := 0; index < 2; index++ {
		entry := agentEntry[(startIndex+index)%len(agentEntry)]
		plans = append(plans, zonenpc.SpawnPlan{
			ObjectID: firstObjectID + uint32(index),
			NounName: entry.NounName, Position: addMarker[index].Position,
			Rotation:      addMarker[index].Rotation,
			LocusID:       publication.TriggerMarkerID,
			Kind:          sim.DirectorLocusBoss,
			MarkerSetName: publication.MarkerSetName,
			NPCProfile:    entry.NPCProfile,
		})
	}
	return plans, firstObjectID + uint32(len(plans)), nil
}

func InitialDeveloperPublication(
	director game.CampaignDirector,
) (game.CampaignDirectorPublication, error) {
	var selected game.CampaignDirectorPublication
	var bossMarker game.CampaignDirectorMarker
	bossMarkerSet := game.CampaignDirectorMarkerSet{}
	for _, markerSet := range director.MarkerSets {
		if !strings.EqualFold(markerSet.Name, InitialMarkerSetName) {
			continue
		}
		bossMarkerSet = markerSet
		for _, marker := range markerSet.Markers {
			if strings.EqualFold(
				marker.NounName, "SpawnPoint_DirectorBoss.Noun",
			) {
				bossMarker = marker
			}
		}
		for _, trigger := range markerSet.Triggers {
			for _, event := range trigger.Events {
				if event.CallbackName != InitialTriggerCallback {
					continue
				}
				if selected.TriggerMarkerID != 0 {
					return game.CampaignDirectorPublication{},
						errors.New("boss developer publication ambiguous")
				}
				selected = initialPublication(markerSet, trigger, event)
			}
		}
	}
	if selected.TriggerMarkerID != 0 {
		return selected, nil
	}
	if bossMarkerSet.Name == "" || bossMarker.MarkerID == 0 {
		return game.CampaignDirectorPublication{},
			errors.New("boss developer publication unavailable")
	}
	return game.CampaignDirectorPublication{
		MarkerSetOrdinal: bossMarkerSet.Ordinal,
		MarkerSetName:    bossMarkerSet.Name,
		TriggerOrdinal:   bossMarker.Ordinal,
		TriggerMarkerID:  bossMarker.MarkerID,
		TriggerName:      bossMarker.Name,
		EventName:        "boss triggered",
		CallbackName:     InitialTriggerCallback,
	}, nil
}

func ValidateInitialOrder(hordeSession *zonehorde.Session) error {
	if hordeSession == nil {
		return ErrSecondHordeIncomplete
	}
	if hordeSession.IsComplete("zelems_1_Ai_Horde_2.Markerset") {
		return nil
	}
	// A first-clear route can enter the final arena without intersecting the
	// tiny Horde 2 trigger sphere. Do not leave the authored support boundary
	// retrying forever after the earlier horde has conclusively completed.
	if !hordeSession.IsComplete("zelems_1_Ai_Horde_1.Markerset") {
		return ErrSecondHordeIncomplete
	}
	return nil
}

func initialMarkers(
	director game.CampaignDirector,
	publication game.CampaignDirectorPublication,
) (game.CampaignDirectorMarker, []game.CampaignDirectorMarker, error) {
	var bossMarker game.CampaignDirectorMarker
	for _, markerSet := range director.MarkerSets {
		if markerSet.Ordinal != publication.MarkerSetOrdinal ||
			!strings.EqualFold(markerSet.Name, publication.MarkerSetName) {
			continue
		}
		for _, marker := range markerSet.Markers {
			switch {
			case strings.EqualFold(
				marker.NounName, "SpawnPoint_DirectorBoss.Noun",
			):
				bossMarker = marker
			}
		}
	}
	addMarker, err := initialAddMarkers(director, publication)
	if err != nil {
		return game.CampaignDirectorMarker{}, nil,
			fmt.Errorf("addMarkers: %w", err)
	}
	if bossMarker.MarkerID == 0 ||
		!zonepopulation.IsFinitePosition(bossMarker.Position) {
		return game.CampaignDirectorMarker{}, nil,
			errors.New("incomplete")
	}
	return bossMarker, addMarker, nil
}

func initialAddMarkers(
	director game.CampaignDirector,
	publication game.CampaignDirectorPublication,
) ([]game.CampaignDirectorMarker, error) {
	addMarker := make([]game.CampaignDirectorMarker, 0, 4)
	for _, markerSet := range director.MarkerSets {
		if markerSet.Ordinal != publication.MarkerSetOrdinal ||
			!strings.EqualFold(markerSet.Name, publication.MarkerSetName) {
			continue
		}
		for _, marker := range markerSet.Markers {
			if isInitialAddMarker(marker) {
				addMarker = append(addMarker, marker)
			}
		}
	}
	if len(addMarker) != 4 {
		return nil, errors.New("incomplete")
	}
	slices.SortFunc(
		addMarker,
		func(
			left game.CampaignDirectorMarker,
			right game.CampaignDirectorMarker,
		) int {
			return left.Ordinal - right.Ordinal
		},
	)
	return addMarker, nil
}

func eligibleInitialAgents(
	director game.CampaignDirector,
) []game.CampaignDirectorEntry {
	authoredEntry := zonepopulation.PoolEntries(director, "agent")
	eligibleEntry := make([]game.CampaignDirectorEntry, 0, len(authoredEntry))
	for _, entry := range authoredEntry {
		if !entry.IsHordeLegal || entry.NounName == "" ||
			!entry.NPCProfile.IsKnown {
			continue
		}
		eligibleEntry = append(eligibleEntry, entry)
	}
	return eligibleEntry
}

func isInitialAddMarker(marker game.CampaignDirectorMarker) bool {
	if !strings.EqualFold(
		marker.NounName, "SpawnPoint_DirectorHorde.Noun",
	) {
		return false
	}
	for _, event := range marker.Events {
		if event.EventName == "boss triggered" &&
			event.CallbackName == "HordeSpawner_Register" {
			return true
		}
	}
	return false
}

func initialPublication(
	markerSet game.CampaignDirectorMarkerSet,
	trigger game.CampaignDirectorTrigger,
	event game.CampaignDirectorEvent,
) game.CampaignDirectorPublication {
	publication := game.CampaignDirectorPublication{
		MarkerSetOrdinal: markerSet.Ordinal,
		MarkerSetName:    markerSet.Name, TriggerOrdinal: trigger.Ordinal,
		TriggerMarkerID: trigger.MarkerID, TriggerName: trigger.Name,
		EventOrdinal: event.Ordinal, EventName: event.EventName,
		CallbackName: event.CallbackName,
	}
	for _, marker := range markerSet.Markers {
		for _, listener := range marker.Events {
			if !strings.EqualFold(listener.EventName, event.EventName) {
				continue
			}
			publication.Listeners = append(
				publication.Listeners,
				game.CampaignDirectorListenerPublication{
					MarkerSetOrdinal: markerSet.Ordinal,
					MarkerSetName:    markerSet.Name,
					MarkerOrdinal:    marker.Ordinal,
					MarkerID:         marker.MarkerID, MarkerName: marker.Name,
					NounName: marker.NounName, SpawnKind: marker.SpawnKind,
					PoolKind:         marker.PoolKind,
					IsSpawnKindKnown: marker.IsSpawnKindKnown,
					Position:         marker.Position, EventOrdinal: listener.Ordinal,
					Rotation:     marker.Rotation,
					CallbackName: listener.CallbackName,
				},
			)
		}
	}
	return publication
}
