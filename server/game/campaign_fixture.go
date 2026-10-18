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
const infinityFoundryLevel = "infinity_2"

// CampaignTreeObjects retains the selected map's authored tree callbacks.
// Legacy unselected Nocturna layouts compose roots around resolved vines.
func (e CampaignDirector) CampaignTreeObjects(
	selectionID uint32,
) ([]CampaignScriptObject, error) {
	const callbackName = "nLevelObject.OnTreeDeath"
	if e.IsInitialLayoutSelected || !e.isNocturnaLevel() {
		objects, err := e.CampaignCallbackObjects(selectionID, callbackName)
		if err != nil {
			return nil, fmt.Errorf("treeObjects: %w", err)
		}
		return objects, nil
	}
	var vineMarkers []CampaignDirectorMarker
	var err error
	if strings.EqualFold(e.Level, "nocturna_1") ||
		strings.EqualFold(e.Level, "nocturna_2") ||
		strings.EqualFold(e.Level, "nocturna_3") ||
		strings.EqualFold(e.Level, "nocturna_4") {
		vineMarkers, err = e.NocturnaSelectedVines(selectionID)
	} else {
		vineMarkers, err = e.NightmareVineFixtures()
	}
	if err != nil {
		return nil, fmt.Errorf("treeVines: %w", err)
	}
	if len(vineMarkers) == 0 {
		objects, callbackErr := e.CampaignCallbackObjects(selectionID, callbackName)
		if callbackErr != nil {
			return nil, fmt.Errorf("treeObjects: %w", callbackErr)
		}
		return objects, nil
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
			if strings.EqualFold(object.MarkerSetName, vineMarker.MarkerSetName) &&
				areCampaignPositionsNear(object.Position, vineMarker.Position, 10) {
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

// NocturnaFixtures returns every authored combat destructible in the composed
// smart-object layout. Nightmare Vines keep their weighted tree replacement
// rule, while explosive and supernatural plants retain each unique placement.
func (e CampaignDirector) NocturnaFixtures() ([]CampaignDirectorMarker, error) {
	if !e.isNocturnaLevel() {
		return nil, nil
	}
	vineMarkers, err := e.NightmareVineFixtures()
	if err != nil {
		return nil, fmt.Errorf("nocturnaVines: %w", err)
	}
	selectedMarkers := append([]CampaignDirectorMarker(nil), vineMarkers...)
	for _, markerSet := range e.MarkerSets {
		if !e.isNocturnaFixtureMarkerSet(markerSet.Name) {
			continue
		}
		for _, marker := range markerSet.Markers {
			if !isNocturnaPlantFixture(marker) {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) ||
				!marker.NPCProfile.IsKnown || marker.NPCProfile.HitPoint <= 0 {
				return nil, fmt.Errorf("plantMarker[%d]: invalid", marker.Ordinal)
			}
			isDuplicate := false
			for _, selectedMarker := range selectedMarkers {
				if strings.EqualFold(selectedMarker.NounName, marker.NounName) &&
					areCampaignPositionsNear(selectedMarker.Position, marker.Position, 1) {
					isDuplicate = true
					break
				}
			}
			if isDuplicate {
				continue
			}
			selectedMarkers = append(selectedMarkers, marker)
		}
	}
	return selectedMarkers, nil
}

// NightmareVineFixtures composes the three authored smart-object sets at each
// tree placement. A normal tree wins when it occupies more variants; otherwise
// one destructible vine represents the deduplicated placement.
func (e CampaignDirector) NightmareVineFixtures() ([]CampaignDirectorMarker, error) {
	if !e.isNocturnaLevel() {
		return nil, nil
	}
	candidateMarkers := make([]CampaignDirectorMarker, 0)
	for _, markerSet := range e.MarkerSets {
		if !e.isNocturnaSmartObjectMarkerSet(markerSet.Name) {
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
		return nil, nil
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
			if isVerdanthCombatFixture(marker) {
				// VerdanthFixtures replaces authored destructible IDs separately.
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

// VerdanthFixtures restores fixed and selected-layout destructibles at their
// authored scales. Remove original IDs so another client layout cannot leave
// duplicate or unbreakable scenery behind.
func (e CampaignDirector) VerdanthFixtures(selectionID uint32) (
	[]CampaignDirectorMarker, []uint32, error,
) {
	level := strings.ToLower(e.Level)
	if !strings.HasPrefix(level, "verdanth_") {
		return nil, nil, nil
	}
	selectedSet := fmt.Sprintf("%s_smart_objects_%d.markerset", level, selectionID%3+1)
	if level == verdanthCypressLevel {
		selectedSet = verdanthSceneryMarkerSet
	}
	fixtures := make([]CampaignDirectorMarker, 0)
	deletedObjectIDs := make([]uint32, 0)
	seenMarkerIDs := make(map[uint32]bool)
	for _, markerSet := range e.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		isSelected := !strings.HasPrefix(name, level+"_smart_objects_") ||
			name == selectedSet
		for _, marker := range markerSet.Markers {
			if !isVerdanthCombatFixture(marker) {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) {
				return nil, nil, fmt.Errorf("fixtureMarker[%d]: invalid", marker.Ordinal)
			}
			deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
			if !isSelected || seenMarkerIDs[marker.MarkerID] {
				continue
			}
			if !marker.NPCProfile.IsKnown || !marker.NPCProfile.IsTargetable ||
				marker.NPCProfile.HitPoint <= 0 ||
				!isFiniteCampaignPosition(marker.Rotation) || marker.Scale <= 0 {
				return nil, nil, fmt.Errorf("fixtureProfile[%d]: invalid", marker.MarkerID)
			}
			seenMarkerIDs[marker.MarkerID] = true
			fixtures = append(fixtures, marker)
		}
	}
	return fixtures, deletedObjectIDs, nil
}

func isVerdanthTotemFixture(marker CampaignDirectorMarker) bool {
	switch strings.ToLower(strings.TrimSpace(marker.NounName)) {
	case "dest_tota_headstatue_b.noun", "dest_tota_headstatue_c.noun":
		return true
	default:
		return false
	}
}

func isVerdanthCombatFixture(marker CampaignDirectorMarker) bool {
	if isVerdanthTotemFixture(marker) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(marker.NounName)) {
	case "dest_tota_heroplant_p3_b.noun", "dest_prefab_tota_heroplant_p3_b.noun":
		return true
	default:
		return false
	}
}

// CryosCaveScenery projects one complete authored cave layout for 3-2. Combat
// fixtures are projected separately from scenery.
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
			if isCryosCaveFixtureNoun(marker.NounName) {
				continue
			}
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

// NocturnaScenery selects one authored Obelisk layout. Nocturna 1-3 selects
// one smart-object layout; other Nocturna levels retain their composed scenery.
func (e CampaignDirector) NocturnaScenery(selectionID uint32) (
	[]CampaignDirectorMarker, []uint32, error,
) {
	if !e.isNocturnaLevel() {
		return nil, nil, nil
	}
	levelName := strings.ToLower(strings.TrimSpace(e.Level))
	variant := selectionID%3 + 1
	selectedObeliskName := fmt.Sprintf("%s_obelisk_%d.markerset", levelName, variant)
	selectedSmartObjectName := e.nocturnaSelectedMarkerSet(selectionID)
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
	smartObjectMarkerSetCount := 0
	for _, markerSet := range e.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		if levelName == "nocturna_2" && name == "nocturna_2_design.markerset" {
			for _, marker := range markerSet.Markers {
				if marker.MarkerID != 0 && isNocturnaPlantFixture(marker) {
					deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
				}
			}
			continue
		}
		isObelisk := strings.HasPrefix(name, levelName+"_obelisk_")
		isSmartObject := e.isNocturnaSmartObjectMarkerSet(name)
		isPlantSet := e.isNocturnaPlantMarkerSet(name)
		if !isObelisk && !isSmartObject && !isPlantSet {
			continue
		}
		markerSetCount++
		if isSmartObject {
			smartObjectMarkerSetCount++
		}
		for _, marker := range markerSet.Markers {
			_, isScriptMarker := scriptMarkerIDs[marker.MarkerID]
			if (levelName == "nocturna_1" || levelName == "nocturna_2" ||
				levelName == "nocturna_3" ||
				levelName == "nocturna_4") && isSmartObject {
				isCombatFixture := isNocturnaCombatFixture(marker)
				isRoot := isNocturnaRootNoun(marker.NounName)
				isScenery := isCampaignSceneryMarker(marker)
				if marker.MarkerID != 0 && (isCombatFixture || isRoot || isScenery) {
					deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
				}
				if name == selectedSmartObjectName && isScenery &&
					!isCombatFixture && !isRoot && !isScriptMarker {
					selected = append(selected, marker)
				}
				continue
			}
			if marker.MarkerID != 0 && isNocturnaCombatFixture(marker) {
				deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
				continue
			}
			if isScriptMarker || !isCampaignSceneryMarker(marker) {
				continue
			}
			if isObelisk && name != selectedObeliskName {
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
	if markerSetCount == 0 || smartObjectMarkerSetCount == 0 ||
		len(selected)+len(deletedObjectIDs) == 0 {
		return nil, nil, fmt.Errorf(
			"nocturnaSceneryComposition: sets=%d smart_sets=%d selected=%d deleted=%d",
			markerSetCount, smartObjectMarkerSetCount, len(selected), len(deletedObjectIDs),
		)
	}
	return selected, deletedObjectIDs, nil
}

func isNocturnaCombatFixture(marker CampaignDirectorMarker) bool {
	return strings.EqualFold(
		marker.NounName, "DEST_nocturna_herotree_yellow_1.Noun",
	) || isNocturnaPlantFixture(marker)
}

func isNocturnaPlantFixture(marker CampaignDirectorMarker) bool {
	switch strings.ToLower(strings.TrimSpace(marker.NounName)) {
	case "dest_nocturna_plant_expl.noun", "dest_nocturna_plant_supnat.noun",
		"dest_prefab_nocturna_plant_supnat.noun":
		return true
	default:
		return false
	}
}

// InfinityScenery projects matching authored Obelisk and smart-object layouts.
// The shipped client can otherwise retain scenery from other variants,
// including large structures that do not exist in server navigation.
func (e CampaignDirector) InfinityScenery(selectionID uint32) (
	[]CampaignDirectorMarker, []uint32, error,
) {
	isFoundry := strings.EqualFold(e.Level, infinityFoundryLevel)
	isFactory := strings.EqualFold(e.Level, "infinity_3")
	isCitadelOne := strings.EqualFold(e.Level, "infinity_1")
	isCitadelFour := strings.EqualFold(e.Level, "infinity_4")
	if !isFoundry && !isFactory && !isCitadelOne && !isCitadelFour {
		return nil, nil, nil
	}
	variant := selectionID%3 + 1
	selectedNames := make(map[string]struct{})
	if isFoundry {
		selectedNames[fmt.Sprintf("infinity_2_obelisk_%d.markerset", variant)] = struct{}{}
		selectedNames[fmt.Sprintf("infinity_2_smart_object_%d.markerset", variant)] = struct{}{}
	} else if isFactory {
		selectedNames[fmt.Sprintf("infinity_3_smart_objects_%d.markerset", variant)] = struct{}{}
	} else if isCitadelOne {
		selectedNames[fmt.Sprintf("infinity_1_obelisk_%d.markerset", variant)] = struct{}{}
		selectedNames[fmt.Sprintf("infinity_1_smart_objects_%d.markerset", variant)] = struct{}{}
	} else {
		selectedNames[fmt.Sprintf("infinity_4_obelisk_%d.markerset", variant)] = struct{}{}
		selectedNames[fmt.Sprintf("infinity_4_smart_objects_%d.markerset", variant)] = struct{}{}
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
		isFoundryVariant := isFoundry && (strings.HasPrefix(name, "infinity_2_obelisk_") ||
			strings.HasPrefix(name, "infinity_2_smart_object_"))
		isFactoryVariant := isFactory && strings.HasPrefix(name, "infinity_3_smart_objects_")
		isCitadelOneVariant := isCitadelOne && (strings.HasPrefix(name, "infinity_1_obelisk_") ||
			strings.HasPrefix(name, "infinity_1_smart_objects_"))
		isCitadelFourVariant := isCitadelFour && (strings.HasPrefix(name, "infinity_4_obelisk_") ||
			strings.HasPrefix(name, "infinity_4_smart_objects_"))
		if !isFoundryVariant && !isFactoryVariant && !isCitadelOneVariant && !isCitadelFourVariant {
			continue
		}
		markerSetCount++
		for _, marker := range markerSet.Markers {
			_, isScriptMarker := scriptMarkerIDs[marker.MarkerID]
			if (isFoundry && isInfinityFixtureNoun(marker.NounName)) ||
				(isFactory && isInfinityThreeFixtureNoun(marker.NounName)) ||
				(isCitadelOne && isInfinityOneFixtureNoun(marker.NounName)) ||
				(isCitadelFour && isInfinityFourFixtureNoun(marker.NounName)) {
				continue
			}
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
	expectedMarkerSetCount := 6
	if isFactory {
		expectedMarkerSetCount = 3
	}
	if markerSetCount != expectedMarkerSetCount || len(selected) == 0 || len(deletedObjectIDs) == 0 {
		return nil, nil, fmt.Errorf(
			"infinitySceneryComposition: sets=%d selected=%d deleted=%d",
			markerSetCount, len(selected), len(deletedObjectIDs),
		)
	}
	return selected, deletedObjectIDs, nil
}

// CryosSmartScenery projects a selected Cryos smart layout's noncombat markers.
func (e CampaignDirector) CryosSmartScenery(selectionID uint32) (
	[]CampaignDirectorMarker, []uint32, error,
) {
	markerSetPrefix := ""
	isCryosOne := false
	switch {
	case strings.EqualFold(e.Level, cryosGeyserLevel):
		markerSetPrefix = "cryos_1_smart_object_"
		isCryosOne = true
	case strings.EqualFold(e.Level, "cryos_2"):
		markerSetPrefix = "cryos_2_smart_objects_"
	default:
		return nil, nil, nil
	}
	selectedSet := fmt.Sprintf("%s%d.markerset", markerSetPrefix, selectionID%3+1)
	selected := make([]CampaignDirectorMarker, 0)
	deletedObjectIDs := make([]uint32, 0)
	scriptMarkerIDs := make(map[uint32]struct{}, len(e.Scripts))
	for _, script := range e.Scripts {
		scriptMarkerIDs[script.MarkerID] = struct{}{}
	}
	markerSetCount := 0
	for _, markerSet := range e.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		if !strings.HasPrefix(name, markerSetPrefix) {
			continue
		}
		markerSetCount++
		for _, marker := range markerSet.Markers {
			_, isScriptMarker := scriptMarkerIDs[marker.MarkerID]
			isCombatFixture := (isCryosOne && isCryosOneFixtureNoun(marker.NounName)) ||
				(!isCryosOne && isCryosTwoFixtureNoun(marker.NounName))
			if isScriptMarker || isCombatFixture ||
				!isCampaignSceneryMarker(marker) {
				continue
			}
			if name == selectedSet {
				selected = append(selected, marker)
				continue
			}
			deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
		}
	}
	if markerSetCount != 3 || len(selected) == 0 || len(deletedObjectIDs) == 0 {
		return nil, nil, fmt.Errorf("cryosOneSceneryComposition: sets=%d selected=%d deleted=%d",
			markerSetCount, len(selected), len(deletedObjectIDs))
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
		if !strings.EqualFold(markerSet.Name, markerSetName) &&
			!(strings.EqualFold(e.Level, cryosGeyserLevel) &&
				strings.HasPrefix(strings.ToLower(markerSet.Name), "cryos_1_smart_object_")) &&
			!(strings.EqualFold(e.Level, cryosCaveLevel) &&
				strings.EqualFold(markerSet.Name, "cryos_3_design.markerset")) {
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
	if e.IsInitialLayoutSelected {
		return e
	}
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
	if !e.isNocturnaLevel() {
		return false
	}
	if !strings.HasPrefix(strings.ToLower(marker.NounName), "moon1_tree_") {
		return false
	}
	for _, markerSet := range e.MarkerSets {
		if !e.isNocturnaSmartObjectMarkerSet(markerSet.Name) {
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
	if !e.isNocturnaLevel() {
		return false
	}
	vineCount := 0
	normalTreeCount := 0
	for _, markerSet := range e.MarkerSets {
		if !e.isNocturnaSmartObjectMarkerSet(markerSet.Name) {
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

func (e CampaignDirector) isNocturnaLevel() bool {
	levelName := strings.ToLower(strings.TrimSpace(e.Level))
	return strings.HasPrefix(levelName, "nocturna_")
}

func (e CampaignDirector) isNocturnaSmartObjectMarkerSet(markerSetName string) bool {
	if !e.isNocturnaLevel() {
		return false
	}
	levelName := strings.ToLower(strings.TrimSpace(e.Level))
	name := strings.ToLower(strings.TrimSpace(markerSetName))
	return strings.HasPrefix(name, levelName+"_smart_object_") ||
		strings.HasPrefix(name, levelName+"_smart_objects_")
}

func (e CampaignDirector) isNocturnaPlantMarkerSet(markerSetName string) bool {
	if !e.isNocturnaLevel() {
		return false
	}
	levelName := strings.ToLower(strings.TrimSpace(e.Level))
	name := strings.ToLower(strings.TrimSpace(markerSetName))
	return name == levelName+"_plants.markerset"
}

func (e CampaignDirector) isNocturnaFixtureMarkerSet(markerSetName string) bool {
	return e.isNocturnaSmartObjectMarkerSet(markerSetName) ||
		e.isNocturnaPlantMarkerSet(markerSetName)
}
