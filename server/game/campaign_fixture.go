package game

import (
	"fmt"
	"strings"
)

// NightmareVineFixtures keeps the destructible trees in the same authored
// variant as their root scenery. Trees have no level-script callback, so they
// must be loaded from director markers rather than ScriptObjects.
// Root scenery remains in LevelScriptObjects; these fixtures supplement it.
func (e CampaignDirector) NightmareVineFixtures(
	matchID uint32,
) ([]CampaignDirectorMarker, error) {
	rootObjects, err := e.CampaignCallbackObjects(matchID, "nLevelObject.OnTreeDeath")
	if err != nil {
		return nil, fmt.Errorf("vineRoots: %w", err)
	}
	if len(rootObjects) == 0 {
		return nil, nil
	}
	selectedOrdinal := rootObjects[0].MarkerSetOrdinal
	markers := make([]CampaignDirectorMarker, 0)
	for _, markerSet := range e.MarkerSets {
		if markerSet.Ordinal != selectedOrdinal {
			continue
		}
		for _, marker := range markerSet.Markers {
			if !strings.EqualFold(marker.NounName, "DEST_nocturna_herotree_yellow_1.Noun") {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) ||
				!marker.NPCProfile.IsKnown || marker.NPCProfile.HitPoint <= 0 {
				return nil, fmt.Errorf("vineMarker[%d]: invalid", marker.Ordinal)
			}
			markers = append(markers, marker)
		}
	}
	return markers, nil
}
