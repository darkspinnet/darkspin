package game

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

const verdanthCypressLevel = "verdanth_3"
const verdanthSceneryMarkerSet = "verdanth_3_smart_objects_1.markerset"
const cryosCaveLevel = "cryos_3"
const cryosCaveSceneryMarkerSet = "cryos_3_smart_object_3.markerset"
const cryosGeyserLevel = "cryos_1"
const cryosGeyserMarkerSet = "cryos_1_objects.markerset"
const nocturnaForestLevel = "nocturna_1"
const infinityFoundryLevel = "infinity_2"

// CampaignTreeObjects composes Nocturna's authored root clusters around the
// placements resolved as Nightmare Vines. Other levels retain their authored
// weighted callback selection.
func (e CampaignDirector) CampaignTreeObjects(
	selectionID uint32,
) ([]CampaignScriptObject, error) {
	const callbackName = "nLevelObject.OnTreeDeath"
	if !strings.EqualFold(e.Level, nocturnaForestLevel) {
		objects, err := e.CampaignCallbackObjects(selectionID, callbackName)
		if err != nil {
			return nil, fmt.Errorf("treeObjects: %w", err)
		}
		return objects, nil
	}
	vineMarkers, err := e.NightmareVineFixtures()
	if err != nil {
		return nil, fmt.Errorf("treeVines: %w", err)
	}
	objects, err := e.ScriptObjects()
	if err != nil {
		return nil, fmt.Errorf("treeScriptObjects: %w", err)
	}
	type treeIdentity struct {
		nounName           string
		position           Vec3
		rotation           Vec3
		scale              float32
		isVisible          bool
		isCollisionEnabled bool
	}
	selectedObjects := make([]CampaignScriptObject, 0)
	selectedIdentities := make(map[treeIdentity]struct{})
	for _, object := range objects {
		if !slices.Contains(object.CallbackNames, callbackName) {
			continue
		}
		isVineRoot := false
		for _, vineMarker := range vineMarkers {
			if areCampaignPositionsNear(object.Position, vineMarker.Position, 10) {
				isVineRoot = true
				break
			}
		}
		if !isVineRoot {
			continue
		}
		identity := treeIdentity{
			nounName: strings.ToLower(object.NounName), position: object.Position,
			rotation: object.Rotation, scale: object.Scale,
			isVisible:          object.IsVisible,
			isCollisionEnabled: object.IsCollisionEnabled,
		}
		_, isDuplicate := selectedIdentities[identity]
		if isDuplicate {
			continue
		}
		selectedIdentities[identity] = struct{}{}
		selectedObjects = append(selectedObjects, object)
	}
	if len(selectedObjects) == 0 {
		return nil, errors.New("treeRoots: empty")
	}
	return selectedObjects, nil
}

// NightmareVineFixtures composes the three authored smart-object sets at each
// tree placement. A normal tree wins when it occupies more variants; otherwise
// one destructible vine represents the deduplicated placement.
func (e CampaignDirector) NightmareVineFixtures() ([]CampaignDirectorMarker, error) {
	if !strings.EqualFold(e.Level, nocturnaForestLevel) {
		return nil, nil
	}
	candidateMarkers := make([]CampaignDirectorMarker, 0)
	for _, markerSet := range e.MarkerSets {
		if !strings.HasPrefix(strings.ToLower(markerSet.Name), "nocturna_1_smart_object_") {
			continue
		}
		for _, marker := range markerSet.Markers {
			if !strings.EqualFold(
				marker.NounName, "DEST_nocturna_herotree_yellow_1.Noun",
			) {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) ||
				!marker.NPCProfile.IsKnown || marker.NPCProfile.HitPoint <= 0 {
				return nil, fmt.Errorf("vineMarker[%d]: invalid", marker.Ordinal)
			}
			candidateMarkers = append(candidateMarkers, marker)
		}
	}
	if len(candidateMarkers) == 0 {
		return nil, errors.New("vineMarkers: empty")
	}
	selectedMarkers := make([]CampaignDirectorMarker, 0, len(candidateMarkers))
	for _, candidateMarker := range candidateMarkers {
		if !e.isNocturnaVinePosition(candidateMarker.Position) {
			continue
		}
		isDuplicate := false
		for _, selectedMarker := range selectedMarkers {
			if areCampaignPositionsNear(candidateMarker.Position, selectedMarker.Position, 1) {
				isDuplicate = true
				break
			}
		}
		if isDuplicate {
			continue
		}
		selectedMarkers = append(selectedMarkers, candidateMarker)
	}
	if len(selectedMarkers) == 0 {
		return nil, errors.New("vineComposition: empty")
	}
	return selectedMarkers, nil
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

// NocturnaScenery selects one authored Obelisk layout and composes the three
// smart-object sets into their deduplicated union. The smart-object sets repeat
// shared scenery while also contributing unique environment models.
func (e CampaignDirector) NocturnaScenery(selectionID uint32) (
	[]CampaignDirectorMarker, []uint32, error,
) {
	if !strings.EqualFold(e.Level, nocturnaForestLevel) {
		return nil, nil, nil
	}
	variant := selectionID%3 + 1
	selectedObeliskName := fmt.Sprintf("nocturna_1_obelisk_%d.markerset", variant)
	type sceneryIdentity struct {
		nounName           string
		position           Vec3
		rotation           Vec3
		scale              float32
		isVisible          bool
		isCollisionEnabled bool
	}
	selected := make([]CampaignDirectorMarker, 0)
	deletedObjectIDs := make([]uint32, 0)
	selectedIdentities := make(map[sceneryIdentity]struct{})
	scriptMarkerIDs := make(map[uint32]struct{}, len(e.Scripts))
	for _, script := range e.Scripts {
		scriptMarkerIDs[script.MarkerID] = struct{}{}
	}
	markerSetCount := 0
	for _, markerSet := range e.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		isObelisk := strings.HasPrefix(name, "nocturna_1_obelisk_")
		isSmartObject := strings.HasPrefix(name, "nocturna_1_smart_object_")
		if !isObelisk && !isSmartObject {
			continue
		}
		markerSetCount++
		for _, marker := range markerSet.Markers {
			_, isScriptMarker := scriptMarkerIDs[marker.MarkerID]
			if isScriptMarker || !isCampaignSceneryMarker(marker) {
				continue
			}
			if isObelisk && name != selectedObeliskName {
				deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
				continue
			}
			if isSmartObject && strings.EqualFold(
				marker.NounName, "DEST_nocturna_herotree_yellow_1.Noun",
			) {
				deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
				continue
			}
			if isSmartObject && strings.EqualFold(
				marker.NounName, "moon1_herotree_p1.Noun",
			) && e.isNocturnaVinePosition(marker.Position) {
				deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
				continue
			}
			if isSmartObject && e.isNocturnaVinePlaceholder(marker) {
				deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
				continue
			}
			identity := sceneryIdentity{
				nounName: strings.ToLower(marker.NounName), position: marker.Position,
				rotation: marker.Rotation, scale: marker.Scale,
				isVisible:          marker.IsVisible,
				isCollisionEnabled: marker.IsCollisionEnabled,
			}
			_, isDuplicate := selectedIdentities[identity]
			if isDuplicate {
				deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
				continue
			}
			selectedIdentities[identity] = struct{}{}
			selected = append(selected, marker)
		}
	}
	if markerSetCount != 6 || len(selected) == 0 || len(deletedObjectIDs) == 0 {
		return nil, nil, fmt.Errorf(
			"nocturnaSceneryComposition: sets=%d selected=%d deleted=%d",
			markerSetCount, len(selected), len(deletedObjectIDs),
		)
	}
	return selected, deletedObjectIDs, nil
}

// InfinityScenery projects matching authored Obelisk and smart-object layouts
// for 4-1. The shipped client can otherwise retain scenery from other variants,
// including large structures that do not exist in server navigation.
func (e CampaignDirector) InfinityScenery(selectionID uint32) (
	[]CampaignDirectorMarker, []uint32, error,
) {
	if !strings.EqualFold(e.Level, infinityFoundryLevel) {
		return nil, nil, nil
	}
	variant := selectionID%3 + 1
	selectedNames := map[string]struct{}{
		fmt.Sprintf("infinity_2_obelisk_%d.markerset", variant):      {},
		fmt.Sprintf("infinity_2_smart_object_%d.markerset", variant): {},
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
		isObelisk := strings.HasPrefix(name, "infinity_2_obelisk_")
		isSmartObject := strings.HasPrefix(name, "infinity_2_smart_object_")
		if !isObelisk && !isSmartObject {
			continue
		}
		markerSetCount++
		for _, marker := range markerSet.Markers {
			_, isScriptMarker := scriptMarkerIDs[marker.MarkerID]
			if isScriptMarker || !isCampaignSceneryMarker(marker) {
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
		return nil, nil, fmt.Errorf(
			"infinitySceneryComposition: sets=%d selected=%d deleted=%d",
			markerSetCount, len(selected), len(deletedObjectIDs),
		)
	}
	return selected, deletedObjectIDs, nil
}

// CryosLavaCracks returns the authored crack objects used by Cryos geysers.
func (e CampaignDirector) CryosLavaCracks() []CampaignDirectorMarker {
	markerSetName := ""
	switch {
	case strings.EqualFold(e.Level, cryosCaveLevel):
		markerSetName = cryosCaveSceneryMarkerSet
	case strings.EqualFold(e.Level, cryosGeyserLevel):
		markerSetName = cryosGeyserMarkerSet
	default:
		return nil
	}
	markers := make([]CampaignDirectorMarker, 0)
	for _, markerSet := range e.MarkerSets {
		if !strings.EqualFold(markerSet.Name, markerSetName) {
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

func (e CampaignDirector) isNocturnaVinePlaceholder(marker CampaignDirectorMarker) bool {
	if !strings.HasPrefix(strings.ToLower(marker.NounName), "moon1_tree_") {
		return false
	}
	for _, markerSet := range e.MarkerSets {
		if !strings.HasPrefix(strings.ToLower(markerSet.Name), "nocturna_1_smart_object_") {
			continue
		}
		for _, candidate := range markerSet.Markers {
			if !strings.EqualFold(
				candidate.NounName, "DEST_nocturna_herotree_yellow_1.Noun",
			) || !e.isNocturnaVinePosition(candidate.Position) {
				continue
			}
			if areCampaignPositionsNear(marker.Position, candidate.Position, 5) {
				return true
			}
		}
	}
	return false
}

func areCampaignPositionsNear(first Vec3, second Vec3, maximumDistance float32) bool {
	deltaX := first.X - second.X
	deltaY := first.Y - second.Y
	deltaZ := first.Z - second.Z
	return deltaX*deltaX+deltaY*deltaY+deltaZ*deltaZ <= maximumDistance*maximumDistance
}

func (e CampaignDirector) isNocturnaVinePosition(position Vec3) bool {
	vineCount := 0
	normalTreeCount := 0
	for _, markerSet := range e.MarkerSets {
		if !strings.HasPrefix(strings.ToLower(markerSet.Name), "nocturna_1_smart_object_") {
			continue
		}
		for _, marker := range markerSet.Markers {
			if !areCampaignPositionsNear(position, marker.Position, 1) {
				continue
			}
			if strings.EqualFold(marker.NounName, "DEST_nocturna_herotree_yellow_1.Noun") {
				vineCount++
				continue
			}
			if strings.EqualFold(marker.NounName, "moon1_herotree_p1.Noun") {
				normalTreeCount++
			}
		}
	}
	return vineCount > 0 && vineCount >= normalTreeCount
}
