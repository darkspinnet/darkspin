package game

import (
	"fmt"
	"strings"
)

const CryosFungusNoun = "DEST_prefab_cryos_plants_shascope.Noun"

// CryosOneFixtures retains fixed AC units and geysers with one smart layout.
func (e CampaignDirector) CryosOneFixtures(selectionID uint32) (
	[]CampaignDirectorMarker, []uint32, error,
) {
	if !strings.EqualFold(e.Level, cryosGeyserLevel) {
		return nil, nil, nil
	}
	selectedSet := fmt.Sprintf("cryos_1_smart_object_%d.markerset", selectionID%3+1)
	fixtures := make([]CampaignDirectorMarker, 0)
	deletedObjectIDs := make([]uint32, 0)
	seenMarkerIDs := make(map[uint32]bool)
	isSelectedFound := false
	for _, markerSet := range e.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		isVariant := strings.HasPrefix(name, "cryos_1_smart_object_")
		isSelected := !isVariant || name == selectedSet
		if name == selectedSet {
			isSelectedFound = true
		}
		for _, marker := range markerSet.Markers {
			if !isCryosOneFixtureNoun(marker.NounName) {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) ||
				!isFiniteCampaignPosition(marker.Rotation) || marker.Scale <= 0 {
				return nil, nil, fmt.Errorf("cryosOneMarker[%d]: invalid", marker.Ordinal)
			}
			deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
			if !isSelected || seenMarkerIDs[marker.MarkerID] {
				continue
			}
			if !marker.NPCProfile.IsKnown || !marker.NPCProfile.IsTargetable ||
				marker.NPCProfile.HitPoint <= 0 {
				return nil, nil, fmt.Errorf("cryosOneProfile[%d]: unavailable", marker.MarkerID)
			}
			seenMarkerIDs[marker.MarkerID] = true
			fixtures = append(fixtures, marker)
		}
	}
	if !isSelectedFound || len(fixtures) == 0 || len(deletedObjectIDs) == 0 {
		return nil, nil, fmt.Errorf("cryosOneComposition: set=%t fixtures=%d deleted=%d",
			isSelectedFound, len(fixtures), len(deletedObjectIDs))
	}
	return fixtures, deletedObjectIDs, nil
}

func isCryosOneFixtureNoun(nounName string) bool {
	switch strings.ToLower(strings.TrimSpace(nounName)) {
	case "dest_prefab_cryos_acunit_small_animated.noun",
		"dest_prefab_cryos_acunit_small.noun",
		"dest_prefab_cryos_ice_crack1.noun":
		return true
	default:
		return false
	}
}

// CryosTwoFixtures keeps the fixed AC unit and one authored smart layout.
func (e CampaignDirector) CryosTwoFixtures(selectionID uint32) (
	[]CampaignDirectorMarker, []uint32, error,
) {
	if !strings.EqualFold(e.Level, "cryos_2") {
		return nil, nil, nil
	}
	selectedSet := fmt.Sprintf("cryos_2_smart_objects_%d.markerset", selectionID%3+1)
	fixtures := make([]CampaignDirectorMarker, 0)
	deletedObjectIDs := make([]uint32, 0)
	seenMarkerIDs := make(map[uint32]bool)
	isSelectedFound := false
	for _, markerSet := range e.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		isVariant := strings.HasPrefix(name, "cryos_2_smart_objects_")
		isSelected := !isVariant || name == selectedSet
		if name == selectedSet {
			isSelectedFound = true
		}
		for _, marker := range markerSet.Markers {
			if !isCryosTwoFixtureNoun(marker.NounName) {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) ||
				!isFiniteCampaignPosition(marker.Rotation) || marker.Scale <= 0 {
				return nil, nil, fmt.Errorf("cryosTwoMarker[%d]: invalid", marker.Ordinal)
			}
			deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
			if !isSelected || seenMarkerIDs[marker.MarkerID] {
				continue
			}
			if !marker.NPCProfile.IsKnown || !marker.NPCProfile.IsTargetable ||
				marker.NPCProfile.HitPoint <= 0 {
				return nil, nil, fmt.Errorf("cryosTwoProfile[%d]: unavailable", marker.MarkerID)
			}
			seenMarkerIDs[marker.MarkerID] = true
			fixtures = append(fixtures, marker)
		}
	}
	if !isSelectedFound || len(fixtures) == 0 || len(deletedObjectIDs) == 0 {
		return nil, nil, fmt.Errorf("cryosTwoComposition: set=%t fixtures=%d deleted=%d",
			isSelectedFound, len(fixtures), len(deletedObjectIDs))
	}
	return fixtures, deletedObjectIDs, nil
}

func isCryosTwoFixtureNoun(nounName string) bool {
	switch strings.ToLower(strings.TrimSpace(nounName)) {
	case "dest_prefab_cryos_acunit_small_animated.noun",
		"dest_prefab_cryos_acunit_small.noun",
		"dest_prefab_cryos2_ice_crack1.noun":
		return true
	default:
		return false
	}
}

// CryosFourFixtures uses the fixed AC unit and one authored smart-object
// layout, retiring every client-owned damageable object from all layouts.
func (e CampaignDirector) CryosFourFixtures(selectionID uint32) (
	[]CampaignDirectorMarker, []uint32, error,
) {
	if !strings.EqualFold(e.Level, "cryos_4") {
		return nil, nil, nil
	}
	selectedSet := fmt.Sprintf("cryos_4_smart_objects_%d.markerset", selectionID%3+1)
	fixtures := make([]CampaignDirectorMarker, 0)
	deletedObjectIDs := make([]uint32, 0)
	seenMarkerIDs := make(map[uint32]bool)
	isSelectedFound := false
	for _, markerSet := range e.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		isVariant := strings.HasPrefix(name, "cryos_4_smart_objects_")
		isSelected := !isVariant || name == selectedSet
		if name == selectedSet {
			isSelectedFound = true
		}
		for _, marker := range markerSet.Markers {
			if !isCryosFourFixtureNoun(marker.NounName) {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) ||
				!isFiniteCampaignPosition(marker.Rotation) || marker.Scale <= 0 {
				return nil, nil, fmt.Errorf("cryosFourMarker[%d]: invalid", marker.Ordinal)
			}
			deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
			if !isSelected || seenMarkerIDs[marker.MarkerID] {
				continue
			}
			if !marker.NPCProfile.IsKnown || !marker.NPCProfile.IsTargetable ||
				marker.NPCProfile.HitPoint <= 0 {
				return nil, nil, fmt.Errorf("cryosFourProfile[%d]: unavailable", marker.MarkerID)
			}
			seenMarkerIDs[marker.MarkerID] = true
			fixtures = append(fixtures, marker)
		}
	}
	if !isSelectedFound || len(fixtures) == 0 || len(deletedObjectIDs) == 0 {
		return nil, nil, fmt.Errorf("cryosFourComposition: set=%t fixtures=%d deleted=%d",
			isSelectedFound, len(fixtures), len(deletedObjectIDs))
	}
	return fixtures, deletedObjectIDs, nil
}

func isCryosFourFixtureNoun(nounName string) bool {
	switch strings.ToLower(strings.TrimSpace(nounName)) {
	case "dest_prefab_cryos_ice_crack2.noun",
		"dest_prefab_cryos_acunit_small.noun",
		"dest_prefab_cryos_acunit_small_animated.noun":
		return true
	default:
		return false
	}
}

// CryosCaveFixtures retains the cave's fixed destructibles and the third
// authored layout used by the server's scenery and population projection.
func (e CampaignDirector) CryosCaveFixtures() (
	[]CampaignDirectorMarker, []uint32, error,
) {
	if !strings.EqualFold(e.Level, cryosCaveLevel) {
		return nil, nil, nil
	}
	fixtures := make([]CampaignDirectorMarker, 0)
	deletedObjectIDs := make([]uint32, 0)
	seenMarkerIDs := make(map[uint32]bool)
	isSelectedFound := false
	for _, markerSet := range e.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		isVariant := strings.HasPrefix(name, "cryos_3_smart_object_")
		isSelected := !isVariant || name == cryosCaveSceneryMarkerSet
		if name == cryosCaveSceneryMarkerSet {
			isSelectedFound = true
		}
		for _, marker := range markerSet.Markers {
			if !isCryosCaveFixtureNoun(marker.NounName) {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) ||
				!isFiniteCampaignPosition(marker.Rotation) || marker.Scale <= 0 {
				return nil, nil, fmt.Errorf("cryosCaveMarker[%d]: invalid", marker.Ordinal)
			}
			deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
			if !isSelected || seenMarkerIDs[marker.MarkerID] {
				continue
			}
			if !marker.NPCProfile.IsKnown || !marker.NPCProfile.IsTargetable ||
				marker.NPCProfile.HitPoint <= 0 {
				return nil, nil, fmt.Errorf("cryosCaveProfile[%d]: unavailable", marker.MarkerID)
			}
			seenMarkerIDs[marker.MarkerID] = true
			fixtures = append(fixtures, marker)
		}
	}
	if !isSelectedFound || len(fixtures) == 0 || len(deletedObjectIDs) == 0 {
		return nil, nil, fmt.Errorf("cryosCaveComposition: set=%t fixtures=%d deleted=%d",
			isSelectedFound, len(fixtures), len(deletedObjectIDs))
	}
	return fixtures, deletedObjectIDs, nil
}

func isCryosCaveFixtureNoun(nounName string) bool {
	return strings.EqualFold(nounName, CryosFungusNoun) ||
		strings.EqualFold(nounName, "DEST_prefab_cryos_ice_crack1.Noun")
}
