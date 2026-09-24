package game

import (
	"fmt"
	"strings"
)

const verdanthCypressLevel = "verdanth_3"
const verdanthSceneryMarkerSet = "verdanth_3_smart_objects_1.markerset"
const cryosCaveLevel = "cryos_3"
const cryosCaveSceneryMarkerSet = "cryos_3_smart_object_3.markerset"

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

// VerdanthScenery selects one complete authored smart-object layout for 2-2
// and identifies client-owned objects from the conflicting layouts. The
// shipped client can otherwise present scenery from a different weighted set
// than the server uses for population placement.
func (e CampaignDirector) VerdanthScenery() (
	[]CampaignDirectorMarker, []uint32, error,
) {
	if !strings.EqualFold(e.Level, verdanthCypressLevel) {
		return nil, nil, nil
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
		if name != "verdanth_3_smart_objects_1.markerset" &&
			name != "verdanth_3_smart_objects_2.markerset" &&
			name != "verdanth_3_smart_objects_3.markerset" {
			continue
		}
		markerSetCount++
		for _, marker := range markerSet.Markers {
			_, isScriptMarker := scriptMarkerIDs[marker.MarkerID]
			if isScriptMarker {
				continue
			}
			if !isCampaignSceneryMarker(marker) {
				continue
			}
			if name == verdanthSceneryMarkerSet {
				selected = append(selected, marker)
				continue
			}
			deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
		}
	}
	if markerSetCount != 3 || len(selected) == 0 || len(deletedObjectIDs) == 0 {
		return nil, nil, fmt.Errorf(
			"verdanthSceneryComposition: sets=%d selected=%d deleted=%d",
			markerSetCount, len(selected), len(deletedObjectIDs),
		)
	}
	return selected, deletedObjectIDs, nil
}

// CryosCaveScenery projects one complete authored cave layout for 3-2. The
// third variant contains the lava-crack fixtures used by the cave hazards.
func (e CampaignDirector) CryosCaveScenery() (
	[]CampaignDirectorMarker, []uint32, error,
) {
	if !strings.EqualFold(e.Level, cryosCaveLevel) {
		return nil, nil, nil
	}
	selected := make([]CampaignDirectorMarker, 0)
	deletedObjectIDs := make([]uint32, 0)
	markerSetCount := 0
	for _, markerSet := range e.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		if !strings.HasPrefix(name, "cryos_3_smart_object_") {
			continue
		}
		markerSetCount++
		for _, marker := range markerSet.Markers {
			if !isCampaignSceneryMarker(marker) {
				continue
			}
			if name == cryosCaveSceneryMarkerSet {
				selected = append(selected, marker)
				continue
			}
			deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
		}
	}
	if markerSetCount != 3 || len(selected) == 0 || len(deletedObjectIDs) == 0 {
		return nil, nil, fmt.Errorf(
			"cryosCaveSceneryComposition: sets=%d selected=%d deleted=%d",
			markerSetCount, len(selected), len(deletedObjectIDs),
		)
	}
	return selected, deletedObjectIDs, nil
}

// CryosLavaCracks returns the authored crack objects from the cave layout
// projected by CryosCaveScenery.
func (e CampaignDirector) CryosLavaCracks() []CampaignDirectorMarker {
	if !strings.EqualFold(e.Level, cryosCaveLevel) {
		return nil
	}
	markers := make([]CampaignDirectorMarker, 0)
	for _, markerSet := range e.MarkerSets {
		if !strings.EqualFold(markerSet.Name, cryosCaveSceneryMarkerSet) {
			continue
		}
		for _, marker := range markerSet.Markers {
			if strings.EqualFold(marker.NounName, "DEST_prefab_cryos_ice_crack1.Noun") &&
				marker.MarkerID != 0 && isFiniteCampaignPosition(marker.Position) {
				markers = append(markers, marker)
			}
		}
	}
	return markers
}

// VerdanthPopulationDirector keeps population anchors aligned with the same
// smart-object layout projected to the client.
func (e CampaignDirector) VerdanthPopulationDirector() CampaignDirector {
	if !strings.EqualFold(e.Level, verdanthCypressLevel) {
		return e
	}
	markerSets := make([]CampaignDirectorMarkerSet, 0, len(e.MarkerSets)-2)
	for _, markerSet := range e.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		if strings.HasPrefix(name, "verdanth_3_smart_objects_") &&
			name != verdanthSceneryMarkerSet {
			continue
		}
		markerSets = append(markerSets, markerSet)
	}
	e.MarkerSets = markerSets
	return e
}

func isCampaignSceneryMarker(marker CampaignDirectorMarker) bool {
	if marker.MarkerID == 0 || marker.NounName == "" || len(marker.Events) != 0 ||
		!marker.IsVisible || !isFiniteCampaignPosition(marker.Position) ||
		!isFiniteCampaignPosition(marker.Rotation) || marker.Scale <= 0 {
		return false
	}
	nounName := strings.ToLower(marker.NounName)
	return !strings.HasPrefix(nounName, "spawnpoint_") &&
		!strings.Contains(nounName, "teleporter")
}
