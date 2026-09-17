package npc

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
)

func PlanFixtures(
	markers []game.CampaignDirectorMarker, firstObjectID uint32,
	objectIDLimit uint32,
) ([]SpawnPlan, uint32, error) {
	return planMarkers(markers, firstObjectID, objectIDLimit, true)
}

// PlanActors maps authored fixed NPC placements into ordinary combat actors.
func PlanActors(
	markers []game.CampaignDirectorMarker, firstObjectID uint32,
	objectIDLimit uint32,
) ([]SpawnPlan, uint32, error) {
	return planMarkers(markers, firstObjectID, objectIDLimit, false)
}

func planMarkers(
	markers []game.CampaignDirectorMarker, firstObjectID uint32,
	objectIDLimit uint32, isFixture bool,
) ([]SpawnPlan, uint32, error) {
	if len(markers) == 0 {
		return nil, firstObjectID, errors.New("authored markers empty")
	}
	if objectIDLimit == 0 || firstObjectID == 0 ||
		firstObjectID >= objectIDLimit {
		return nil, firstObjectID, errors.New("fixture object range invalid")
	}
	plans := make([]SpawnPlan, 0, len(markers))
	nextObjectID := firstObjectID
	for markerIndex, currentMarker := range markers {
		if nextObjectID >= objectIDLimit {
			return nil, firstObjectID, fmt.Errorf(
				"fixtureObjectID[%d]: exhausted", markerIndex,
			)
		}
		currentPlan := SpawnPlan{
			ObjectID:         nextObjectID,
			NounName:         currentMarker.NounName,
			AuthoredNounName: currentMarker.AuthoredNounName,
			Position:         currentMarker.Position,
			Rotation:         currentMarker.Rotation,
			LocusID:          currentMarker.MarkerID,
			IsFixture:        isFixture,
			MarkerSetName:    currentMarker.MarkerSetName,
			NPCProfile:       currentMarker.NPCProfile,
		}
		err := ValidateSpawnPlan(currentPlan, objectIDLimit)
		if err != nil {
			return nil, firstObjectID, fmt.Errorf(
				"fixturePlan[%d]: %w", markerIndex, err,
			)
		}
		plans = append(plans, currentPlan)
		nextObjectID++
	}
	return plans, nextObjectID, nil
}
