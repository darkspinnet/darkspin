package game

import (
	"fmt"
	"strings"
)

// InfinityFixtures promotes the selected foundry machinery into server-owned
// combat objects while retiring every authored alternative from the client.
func (e CampaignDirector) InfinityFixtures(selectionID uint32) (
	[]CampaignDirectorMarker, []uint32, error,
) {
	if !strings.EqualFold(e.Level, infinityFoundryLevel) {
		return nil, nil, nil
	}
	selectedSet := fmt.Sprintf("infinity_2_smart_object_%d.markerset", selectionID%3+1)
	fixtures := make([]CampaignDirectorMarker, 0)
	deletedObjectIDs := make([]uint32, 0)
	seenMarkerIDs := make(map[uint32]bool)
	isSelectedFound := false
	for _, markerSet := range e.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		if name == selectedSet {
			isSelectedFound = true
		}
		for _, marker := range markerSet.Markers {
			if !isInfinityFixtureNoun(marker.NounName) {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) ||
				!isFiniteCampaignPosition(marker.Rotation) || marker.Scale <= 0 {
				return nil, nil, fmt.Errorf("infinityMarker[%d]: invalid", marker.Ordinal)
			}
			deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
			if name != selectedSet || seenMarkerIDs[marker.MarkerID] {
				continue
			}
			if !marker.NPCProfile.IsKnown || !marker.NPCProfile.IsTargetable ||
				marker.NPCProfile.HitPoint <= 0 {
				return nil, nil, fmt.Errorf("infinityProfile[%d]: unavailable", marker.MarkerID)
			}
			seenMarkerIDs[marker.MarkerID] = true
			fixtures = append(fixtures, marker)
		}
	}
	if !isSelectedFound || len(fixtures) == 0 || len(deletedObjectIDs) == 0 {
		return nil, nil, fmt.Errorf("infinityComposition: set=%t fixtures=%d deleted=%d",
			isSelectedFound, len(fixtures), len(deletedObjectIDs))
	}
	return fixtures, deletedObjectIDs, nil
}

func isInfinityFixtureNoun(nounName string) bool {
	switch strings.ToLower(strings.TrimSpace(nounName)) {
	case "dest_prefab_citadel_factoryvent.noun",
		"dest_citadel_fuelcanister.noun",
		"dest_prefab_citadel_factorypipe_plasma.noun",
		"dest_prefab_citadel_factorypipe_smoke.noun":
		return true
	default:
		return false
	}
}

// InfinityThreeFixtures selects one factory layout and replaces its authored
// machinery with shared combat fixtures.
func (e CampaignDirector) InfinityThreeFixtures(selectionID uint32) (
	[]CampaignDirectorMarker, []uint32, error,
) {
	if !strings.EqualFold(e.Level, "infinity_3") {
		return nil, nil, nil
	}
	selectedSet := fmt.Sprintf("infinity_3_smart_objects_%d.markerset", selectionID%3+1)
	fixtures := make([]CampaignDirectorMarker, 0)
	deletedObjectIDs := make([]uint32, 0)
	seenMarkerIDs := make(map[uint32]bool)
	isSelectedFound := false
	for _, markerSet := range e.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		if name == selectedSet {
			isSelectedFound = true
		}
		for _, marker := range markerSet.Markers {
			if !isInfinityThreeFixtureNoun(marker.NounName) {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) ||
				!isFiniteCampaignPosition(marker.Rotation) || marker.Scale <= 0 {
				return nil, nil, fmt.Errorf("infinityThreeMarker[%d]: invalid", marker.Ordinal)
			}
			deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
			if name != selectedSet || seenMarkerIDs[marker.MarkerID] {
				continue
			}
			if !marker.NPCProfile.IsKnown || !marker.NPCProfile.IsTargetable ||
				marker.NPCProfile.HitPoint <= 0 {
				return nil, nil, fmt.Errorf("infinityThreeProfile[%d]: unavailable", marker.MarkerID)
			}
			seenMarkerIDs[marker.MarkerID] = true
			fixtures = append(fixtures, marker)
		}
	}
	if !isSelectedFound || len(fixtures) == 0 || len(deletedObjectIDs) == 0 {
		return nil, nil, fmt.Errorf("infinityThreeComposition: set=%t fixtures=%d deleted=%d",
			isSelectedFound, len(fixtures), len(deletedObjectIDs))
	}
	return fixtures, deletedObjectIDs, nil
}

func isInfinityThreeFixtureNoun(nounName string) bool {
	switch strings.ToLower(strings.TrimSpace(nounName)) {
	case "dest_citadel_factorypipe.noun",
		"dest_citadel_fuelcanister.noun",
		"dest_prefab_citadel_factorypipe_plasma.noun",
		"dest_prefab_citadel_factorypipe_smoke.noun":
		return true
	default:
		return false
	}
}

// InfinityOneFixtures keeps fixed boss vents and pipes with one smart layout.
func (e CampaignDirector) InfinityOneFixtures(selectionID uint32) (
	[]CampaignDirectorMarker, []uint32, error,
) {
	if !strings.EqualFold(e.Level, "infinity_1") {
		return nil, nil, nil
	}
	selectedSet := fmt.Sprintf("infinity_1_smart_objects_%d.markerset", selectionID%3+1)
	fixtures := make([]CampaignDirectorMarker, 0)
	deletedObjectIDs := make([]uint32, 0)
	seenMarkerIDs := make(map[uint32]bool)
	isSelectedFound := false
	for _, markerSet := range e.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		isVariant := strings.HasPrefix(name, "infinity_1_smart_objects_")
		isSelected := !isVariant || name == selectedSet
		if name == selectedSet {
			isSelectedFound = true
		}
		for _, marker := range markerSet.Markers {
			if !isInfinityOneFixtureNoun(marker.NounName) {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) ||
				!isFiniteCampaignPosition(marker.Rotation) || marker.Scale <= 0 {
				return nil, nil, fmt.Errorf("infinityOneMarker[%d]: invalid", marker.Ordinal)
			}
			deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
			if !isSelected || seenMarkerIDs[marker.MarkerID] {
				continue
			}
			if !marker.NPCProfile.IsKnown || !marker.NPCProfile.IsTargetable ||
				marker.NPCProfile.HitPoint <= 0 {
				return nil, nil, fmt.Errorf("infinityOneProfile[%d]: unavailable", marker.MarkerID)
			}
			seenMarkerIDs[marker.MarkerID] = true
			fixtures = append(fixtures, marker)
		}
	}
	if !isSelectedFound || len(fixtures) == 0 || len(deletedObjectIDs) == 0 {
		return nil, nil, fmt.Errorf("infinityOneComposition: set=%t fixtures=%d deleted=%d",
			isSelectedFound, len(fixtures), len(deletedObjectIDs))
	}
	return fixtures, deletedObjectIDs, nil
}

func isInfinityOneFixtureNoun(nounName string) bool {
	switch strings.ToLower(strings.TrimSpace(nounName)) {
	case "dest_citadel_factoryvent_boss.noun",
		"dest_citadel_fuelcanister.noun",
		"dest_prefab_citadel_factorypipe_plasma.noun",
		"dest_prefab_citadel_factorypipe_smoke.noun":
		return true
	default:
		return false
	}
}

// InfinityFourFixtures keeps fixed pipes with one authored smart layout.
func (e CampaignDirector) InfinityFourFixtures(selectionID uint32) (
	[]CampaignDirectorMarker, []uint32, error,
) {
	if !strings.EqualFold(e.Level, "infinity_4") {
		return nil, nil, nil
	}
	selectedSet := fmt.Sprintf("infinity_4_smart_objects_%d.markerset", selectionID%3+1)
	fixtures := make([]CampaignDirectorMarker, 0)
	deletedObjectIDs := make([]uint32, 0)
	seenMarkerIDs := make(map[uint32]bool)
	isSelectedFound := false
	for _, markerSet := range e.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		isVariant := strings.HasPrefix(name, "infinity_4_smart_objects_")
		isSelected := !isVariant || name == selectedSet
		if name == selectedSet {
			isSelectedFound = true
		}
		for _, marker := range markerSet.Markers {
			if !isInfinityFourFixtureNoun(marker.NounName) {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) ||
				!isFiniteCampaignPosition(marker.Rotation) || marker.Scale <= 0 {
				return nil, nil, fmt.Errorf("infinityFourMarker[%d]: invalid", marker.Ordinal)
			}
			deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
			if !isSelected || seenMarkerIDs[marker.MarkerID] {
				continue
			}
			if !marker.NPCProfile.IsKnown || !marker.NPCProfile.IsTargetable ||
				marker.NPCProfile.HitPoint <= 0 {
				return nil, nil, fmt.Errorf("infinityFourProfile[%d]: unavailable", marker.MarkerID)
			}
			seenMarkerIDs[marker.MarkerID] = true
			fixtures = append(fixtures, marker)
		}
	}
	if !isSelectedFound || len(fixtures) == 0 || len(deletedObjectIDs) == 0 {
		return nil, nil, fmt.Errorf("infinityFourComposition: set=%t fixtures=%d deleted=%d",
			isSelectedFound, len(fixtures), len(deletedObjectIDs))
	}
	return fixtures, deletedObjectIDs, nil
}

func isInfinityFourFixtureNoun(nounName string) bool {
	switch strings.ToLower(strings.TrimSpace(nounName)) {
	case "dest_citadel_fuelcanister.noun",
		"dest_prefab_citadel_factorypipe_plasma.noun",
		"dest_prefab_citadel_factorypipe_smoke.noun",
		"dest_prefab_citadel_factorypipe.noun",
		"dest_prefab_citadel_factoryvent.noun":
		return true
	default:
		return false
	}
}
