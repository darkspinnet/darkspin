package game

import (
	"fmt"
	"strings"
)

const GraviticStabilizerNoun = "DEST_prefab_islands_instrument_scitech_7.Noun"

func IsGraviticRegulatorNoun(nounName string) bool {
	switch strings.ToLower(nounName) {
	case "dest_prefab_islands_instrument_scitech_2.noun",
		"dest_prefab_islands_instrument_scitech_3.noun",
		"dest_prefab_islands_instrument_scitech_11.noun",
		"dest_prefab_islands_instrument_scitech_11_noshadow.noun":
		return true
	default:
		return false
	}
}

func IsGraviticStabilizerNoun(nounName string) bool {
	return strings.EqualFold(nounName, GraviticStabilizerNoun) ||
		strings.EqualFold(nounName, "DEST_prefab_islands_instrument_scitech_7_noShadow.Noun")
}

// GraviticFixtures uses one authored smart-object variant plus the fixed
// placements. Delete all authored fixture IDs before creating authoritative
// actors so the client's independently selected variant cannot leave ghosts.
func (e CampaignDirector) GraviticFixtures(selectionID uint32) (
	[]CampaignDirectorMarker, []uint32, error,
) {
	level := strings.ToLower(e.Level)
	if level != "zelems_2" && level != "zelems_3" && level != "zelems_4" {
		return nil, nil, nil
	}
	selectedSet := fmt.Sprintf("%s_smart_objects_%d.markerset", level, selectionID%3+1)
	selectedObeliskSet := fmt.Sprintf("%s_obelisks_%d.markerset", level, selectionID%3+1)
	fixtures := make([]CampaignDirectorMarker, 0)
	deletedObjectIDs := make([]uint32, 0)
	seenMarkerIDs := make(map[uint32]bool)
	for _, markerSet := range e.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		isVariant := strings.HasPrefix(name, level+"_smart_objects_") ||
			strings.HasPrefix(name, level+"_obelisks_")
		isSelected := !isVariant || name == selectedSet || name == selectedObeliskSet
		for _, marker := range markerSet.Markers {
			isInstrument := IsGraviticRegulatorNoun(marker.NounName) ||
				IsGraviticStabilizerNoun(marker.NounName)
			if !isInstrument {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) {
				return nil, nil, fmt.Errorf("graviticMarker[%d]: invalid", marker.Ordinal)
			}
			deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
			if !isSelected || seenMarkerIDs[marker.MarkerID] {
				continue
			}
			if !marker.NPCProfile.IsKnown || !marker.NPCProfile.IsTargetable ||
				marker.NPCProfile.HitPoint <= 0 {
				return nil, nil, fmt.Errorf("graviticProfile[%d]: unavailable", marker.MarkerID)
			}
			seenMarkerIDs[marker.MarkerID] = true
			fixtures = append(fixtures, marker)
		}
	}
	return fixtures, deletedObjectIDs, nil
}
