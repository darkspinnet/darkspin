package game

import (
	"fmt"
	"strings"
)

// ScaldronFixtures retains fixed targetable plants and one smart layout.
func (e CampaignDirector) ScaldronFixtures(selectionID uint32) (
	[]CampaignDirectorMarker, []uint32, error,
) {
	levelName := strings.ToLower(strings.TrimSpace(e.Level))
	if levelName != "scaldron_1" && levelName != "scaldron_2" &&
		levelName != "scaldron_3" && levelName != "scaldron_4" {
		return nil, nil, nil
	}
	smartPrefix := levelName + "_smart_objects_"
	if levelName == "scaldron_1" {
		smartPrefix = levelName + "_smart_object_"
	}
	selectedSmartSet := fmt.Sprintf("%s%d.markerset", smartPrefix, selectionID%3+1)
	selectedObeliskSet := fmt.Sprintf("%s_obelisk_%d.markerset", levelName, selectionID%3+1)
	fixtures := make([]CampaignDirectorMarker, 0)
	deletedObjectIDs := make([]uint32, 0)
	seenMarkerIDs := make(map[uint32]bool)
	isSmartFound := false
	isObeliskFound := false
	for _, markerSet := range e.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		isVariant := strings.HasPrefix(name, smartPrefix) ||
			strings.HasPrefix(name, levelName+"_obelisk_")
		isSelected := !isVariant || name == selectedSmartSet || name == selectedObeliskSet
		if name == selectedSmartSet {
			isSmartFound = true
		}
		if name == selectedObeliskSet {
			isObeliskFound = true
		}
		for _, marker := range markerSet.Markers {
			if !isScaldronFixtureNoun(marker.NounName) {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) ||
				!isFiniteCampaignPosition(marker.Rotation) || marker.Scale <= 0 {
				return nil, nil, fmt.Errorf("scaldronMarker[%d]: invalid", marker.Ordinal)
			}
			deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
			if !isSelected || seenMarkerIDs[marker.MarkerID] {
				continue
			}
			if !marker.NPCProfile.IsKnown || !marker.NPCProfile.IsTargetable ||
				marker.NPCProfile.HitPoint <= 0 {
				return nil, nil, fmt.Errorf("scaldronProfile[%d]: unavailable", marker.MarkerID)
			}
			seenMarkerIDs[marker.MarkerID] = true
			fixtures = append(fixtures, marker)
		}
	}
	if !isSmartFound || !isObeliskFound || len(fixtures) == 0 || len(deletedObjectIDs) == 0 {
		return nil, nil, fmt.Errorf("scaldronComposition: smart=%t obelisk=%t fixtures=%d deleted=%d",
			isSmartFound, isObeliskFound, len(fixtures), len(deletedObjectIDs))
	}
	return fixtures, deletedObjectIDs, nil
}

func isScaldronFixtureNoun(nounName string) bool {
	switch strings.ToLower(strings.TrimSpace(nounName)) {
	case "dest_prefab_scaldron_plant_large_1.noun",
		"dest_prefab_scaldron_plant_large_1b.noun",
		"dest_prefab_scaldron_plant_small_3.noun",
		"dest_prefab_scaldron_tem_column_statue.noun":
		return true
	default:
		return false
	}
}

// ScaldronScenery selects matching Obelisk and smart-object scenery.
func (e CampaignDirector) ScaldronScenery(selectionID uint32) (
	[]CampaignDirectorMarker, []uint32, error,
) {
	levelName := strings.ToLower(strings.TrimSpace(e.Level))
	if levelName != "scaldron_1" && levelName != "scaldron_2" &&
		levelName != "scaldron_3" && levelName != "scaldron_4" {
		return nil, nil, nil
	}
	smartPrefix := levelName + "_smart_objects_"
	if levelName == "scaldron_1" {
		smartPrefix = levelName + "_smart_object_"
	}
	variant := selectionID%3 + 1
	selectedNames := map[string]struct{}{
		fmt.Sprintf("%s_obelisk_%d.markerset", levelName, variant): {},
		fmt.Sprintf("%s%d.markerset", smartPrefix, variant):        {},
	}
	selected := make([]CampaignDirectorMarker, 0)
	deletedObjectIDs := make([]uint32, 0)
	scriptMarkerIDs := make(map[uint32]struct{}, len(e.Scripts))
	for _, script := range e.Scripts {
		scriptMarkerIDs[script.MarkerID] = struct{}{}
	}
	markerSetCount := 0
	for _, markerSet := range e.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		if !strings.HasPrefix(name, levelName+"_obelisk_") &&
			!strings.HasPrefix(name, smartPrefix) {
			continue
		}
		markerSetCount++
		for _, marker := range markerSet.Markers {
			_, isScriptMarker := scriptMarkerIDs[marker.MarkerID]
			if isScriptMarker || isScaldronFixtureNoun(marker.NounName) ||
				!isCampaignSceneryMarker(marker) {
				continue
			}
			_, isSelected := selectedNames[name]
			if isSelected {
				selected = append(selected, marker)
				continue
			}
			deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
		}
	}
	if markerSetCount != 6 || len(selected) == 0 || len(deletedObjectIDs) == 0 {
		return nil, nil, fmt.Errorf("scaldronSceneryComposition: sets=%d selected=%d deleted=%d",
			markerSetCount, len(selected), len(deletedObjectIDs))
	}
	return selected, deletedObjectIDs, nil
}
