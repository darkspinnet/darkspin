package game

import (
	"fmt"
	"strings"
)

func (e CampaignDirector) nocturnaSelectedMarkerSet(selectionID uint32) string {
	levelName := strings.ToLower(strings.TrimSpace(e.Level))
	setName := "smart_object"
	if levelName == "nocturna_2" || levelName == "nocturna_3" ||
		levelName == "nocturna_4" {
		setName = "smart_objects"
	}
	return fmt.Sprintf("%s_%s_%d.markerset", levelName, setName, selectionID%3+1)
}

// NocturnaSelectedVines uses the authored vines in the selected object layout.
// Their roots are separate script objects, not combat fixtures.
func (e CampaignDirector) NocturnaSelectedVines(selectionID uint32) (
	[]CampaignDirectorMarker, error,
) {
	if !strings.EqualFold(e.Level, "nocturna_1") &&
		!strings.EqualFold(e.Level, "nocturna_2") &&
		!strings.EqualFold(e.Level, "nocturna_3") &&
		!strings.EqualFold(e.Level, "nocturna_4") {
		return nil, fmt.Errorf("nocturnaSelectedLevel: %q", e.Level)
	}
	selectedSet := e.nocturnaSelectedMarkerSet(selectionID)
	vines := make([]CampaignDirectorMarker, 0)
	isSelectedFound := false
	for _, markerSet := range e.MarkerSets {
		if !strings.EqualFold(markerSet.Name, selectedSet) {
			continue
		}
		isSelectedFound = true
		for _, marker := range markerSet.Markers {
			if !strings.EqualFold(marker.NounName, "DEST_nocturna_herotree_yellow_1.Noun") {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) ||
				!marker.NPCProfile.IsKnown || !marker.NPCProfile.IsTargetable ||
				marker.NPCProfile.HitPoint <= 0 {
				return nil, fmt.Errorf("nocturnaSelectedVine[%d]: invalid", marker.MarkerID)
			}
			vines = append(vines, marker)
		}
	}
	if !isSelectedFound || len(vines) == 0 {
		return nil, fmt.Errorf("nocturnaSelectedVines: set=%t vines=%d", isSelectedFound, len(vines))
	}
	return vines, nil
}

func (e CampaignDirector) NocturnaSelectedFixtures(selectionID uint32) (
	[]CampaignDirectorMarker, error,
) {
	vines, err := e.NocturnaSelectedVines(selectionID)
	if err != nil {
		return nil, fmt.Errorf("nocturnaSelectedFixtureVines: %w", err)
	}
	fixtures := append([]CampaignDirectorMarker(nil), vines...)
	selectedSet := e.nocturnaSelectedMarkerSet(selectionID)
	for _, markerSet := range e.MarkerSets {
		isFixedPlantSet := strings.EqualFold(e.Level, "nocturna_2") &&
			strings.EqualFold(markerSet.Name, "nocturna_2_design.markerset")
		if !strings.EqualFold(markerSet.Name, selectedSet) && !isFixedPlantSet {
			continue
		}
		for _, marker := range markerSet.Markers {
			if !isNocturnaPlantFixture(marker) {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) ||
				!marker.NPCProfile.IsKnown || !marker.NPCProfile.IsTargetable ||
				marker.NPCProfile.HitPoint <= 0 {
				return nil, fmt.Errorf("nocturnaSelectedPlant[%d]: invalid", marker.MarkerID)
			}
			fixtures = append(fixtures, marker)
		}
	}
	return fixtures, nil
}

func isNocturnaRootNoun(nounName string) bool {
	return strings.HasPrefix(strings.ToLower(nounName), "dest_prefab_nocturna_root_")
}
