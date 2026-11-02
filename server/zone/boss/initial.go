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
	// The native delay after ActivateHordeSpawn is not recovered. Immediate
	// local admission is a separate server policy, not a script wait.
	InitialAdmissionDelay  = time.Duration(0)
	InitialSecondWaveDelay = 2 * time.Second
)

var ErrHordeActive = errors.New("boss order: horde still active")

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
	addMarker = expandOpeningMarkers(addMarker, director.BossSpawnRules.Resolved().OpeningAddsPerAnchor)
	if uint32(len(addMarker)+1) > zoneobject.ProjectileIDStart-firstObjectID {
		return nil, firstObjectID, errors.New("boss plan: object IDs exhausted")
	}
	// Named captains belong to the boss roster, not the lieutenant pool used
	// for ordinary population. Use the same prepared roster as admission audit.
	leaderEntries := NamedBossEntries(director, chainLevelIndex)
	agentEntry := bossAddEntries(director)
	if len(agentEntry) == 0 {
		return nil, firstObjectID, errors.New("boss plan: empty pool")
	}
	leaderEntry, err := SelectNamedLeader(
		director, leaderEntries, chainLevelIndex, gameID, bossMarker.MarkerID,
	)
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("boss plan leader: %w", err)
	}
	bossIdentity, isBossIdentityFound := namedBossIdentity(
		director, leaderEntry.NounName,
	)
	if !isBossIdentityFound {
		return nil, firstObjectID, errors.New("boss plan identity: missing")
	}
	leaderEntry.NPCProfile = zonenpc.ApplyCaptainProfile(leaderEntry.NPCProfile, bossIdentity)
	plans := []zonenpc.SpawnPlan{{
		ObjectID: firstObjectID, NounName: leaderEntry.NounName,
		Position: bossMarker.Position, LocusID: bossMarker.MarkerID,
		Rotation: bossMarker.Rotation,
		Kind:     sim.DirectorLocusBoss, IsCaptain: true, IsBoss: true,
		MarkerSetName: publication.MarkerSetName,
		NPCProfile:    leaderEntry.NPCProfile,
		BossIdentity:  bossIdentity,
	}}
	random := initialAddRandom(director, gameID, bossMarker.MarkerID, 1)
	for index, marker := range addMarker {
		entryIndex, choiceErr := random.Index(uint32(len(agentEntry)))
		if choiceErr != nil {
			return nil, firstObjectID, fmt.Errorf("bossAddChoice[%d]: %w", index, choiceErr)
		}
		entry := agentEntry[entryIndex]
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
	agentEntry := bossAddEntries(director)
	if len(agentEntry) == 0 {
		return nil, firstObjectID, errors.New("boss second plan: empty pool")
	}
	random := initialAddRandom(director, gameID, publication.TriggerMarkerID, 2)
	addCount := int(director.BossSpawnRules.Resolved().InitialBossAddCount)
	if uint32(addCount) > zoneobject.ProjectileIDStart-firstObjectID {
		return nil, firstObjectID, errors.New("boss second plan: object IDs exhausted")
	}
	plans := make([]zonenpc.SpawnPlan, 0, addCount)
	for index := 0; index < addCount; index++ {
		marker := addMarker[index%len(addMarker)]
		positions := zonepopulation.GroupPositions(
			[]game.Vec3{marker.Position}, (addCount+len(addMarker)-1)/len(addMarker),
		)
		marker.Position = positions[index/len(addMarker)]
		entryIndex, choiceErr := random.Index(uint32(len(agentEntry)))
		if choiceErr != nil {
			return nil, firstObjectID, fmt.Errorf("bossSecondChoice[%d]: %w", index, choiceErr)
		}
		entry := agentEntry[entryIndex]
		plans = append(plans, zonenpc.SpawnPlan{
			ObjectID: firstObjectID + uint32(index),
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
		bossMarker = initialBossAnchor(markerSet)
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
		return errors.New("boss horde state unavailable")
	}
	// Hordes are alternatives, not mandatory predecessors of the boss. Only
	// an encounter already in progress owns the route; absent/untriggered
	// hordes impose no completion requirement on the final arena trigger.
	if hordeSession.IsActive() {
		return ErrHordeActive
	}
	return nil
}

// Encounter streams are deliberately independent of the native layout draws.
// Phase/marker salts are server policy; they do not claim to reproduce the
// original director's unrecovered challenge-budget call sequence.
func initialAddRandom(
	director game.CampaignDirector, fallbackSeed, markerID, waveOrdinal uint32,
) *sim.SimulatorRandom {
	seed := fallbackSeed
	if director.IsInitialLayoutSelected {
		seed = director.MapVariantSeed
	}
	return sim.NewSimulatorRandom(seed ^ markerID ^ waveOrdinal*0x103 ^ 0x424f5353)
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
		bossMarker = initialBossAnchor(markerSet)
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

// The authored boss anchor also owns a trigger volume. The content loader
// retains that placement in Triggers rather than Markers; both roles still
// refer to the same spawn position and rotation.
func initialBossAnchor(markerSet game.CampaignDirectorMarkerSet) game.CampaignDirectorMarker {
	for _, marker := range markerSet.Markers {
		if strings.EqualFold(marker.NounName, "SpawnPoint_DirectorBoss.Noun") {
			return marker
		}
	}
	for _, trigger := range markerSet.Triggers {
		if !strings.EqualFold(trigger.NounName, "SpawnPoint_DirectorBoss.Noun") {
			continue
		}
		return game.CampaignDirectorMarker{
			Ordinal: trigger.Ordinal, MarkerID: trigger.MarkerID,
			MarkerSetName: markerSet.Name, Name: trigger.Name,
			NounName: trigger.NounName, Position: trigger.Position,
			Rotation: trigger.Rotation,
		}
	}
	return game.CampaignDirectorMarker{}
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
