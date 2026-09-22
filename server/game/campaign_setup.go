package game

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// CampaignDirectorEntry is one difficulty-gated noun candidate from immutable
// level content. It does not assign selection weight or encounter behavior.
type CampaignDirectorEntry struct {
	Ordinal                   int
	ConfigurationEntryOrdinal int
	NounName                  string
	MinimumDifficulty         uint32
	MaximumDifficulty         uint32
	IsHordeLegal              bool
	NPCProfile                CampaignNPCProfile
}

const MaxCampaignNPCAffixCount = 6

// CampaignNPCIdentity is immutable presentation metadata decoded from one
// packaged non-player ClassAttributes resource. Encounter policy decides
// whether that noun is selected as a captain, elite, or boss.
type CampaignNPCIdentity struct {
	DisplayName   string
	NPCAffixNames [MaxCampaignNPCAffixCount]string
	IsKnown       bool
}

// CampaignNPCProfile is the transport- and storage-neutral authored
// baseline for one director noun.
type CampaignNPCProfile struct {
	ChallengeValue         int32
	NPCRank                int32
	IsTargetable           bool
	IsPlayerPet            bool
	PlayerCountHealthScale float32
	HitPoint               float32
	PowerPoint             float32
	Strength               float32
	Dexterity              float32
	Mind                   float32
	DodgeRating            float32
	ResistRating           float32
	CriticalRating         float32
	GraphicsScale          float32
	FootprintRadius        float32
	// DifficultyDamageMultiplier is the already-selected shared difficulty
	// stage. Zero preserves legacy/test profiles and is interpreted as one.
	DifficultyDamageMultiplier float32
	IsKnown                    bool
}

// CampaignFallbackChallenge returns the documented emergency campaign-part
// scaffold used only when an otherwise valid combatant lacks class data.
func CampaignFallbackChallenge(levelName string) int32 {
	stage := strings.Split(strings.TrimSpace(levelName), "-")
	if len(stage) != 2 {
		return 0
	}
	campaign, campaignErr := strconv.ParseInt(stage[0], 10, 32)
	part, partErr := strconv.ParseInt(stage[1], 10, 32)
	if campaignErr != nil || partErr != nil || campaign <= 0 || part <= 0 {
		return 0
	}
	return int32(campaign*10 + part)
}

// CampaignDirectorPool preserves one authored structural pool boundary.
type CampaignDirectorPool struct {
	ConfigurationOrdinal int
	ConfigKind           string
	SpawnKind            string
	Entries              []CampaignDirectorEntry
}

// CampaignDirectorEvent is one authored listener or trigger binding on a
// director marker. Campaign setup carries it without executing it.
type CampaignDirectorEvent struct {
	Ordinal           int
	ComponentName     string
	EventKind         string
	EventSlot         string
	EventName         string
	CallbackName      string
	TriggerRadius     float32
	IsTriggerOnceOnly bool
	IsServerOnly      bool
}

// CampaignDirectorMarker is one authored placement owned by a marker set.
type CampaignDirectorMarker struct {
	Ordinal                 int
	MarkerID                uint32
	MarkerSetName           string
	Name                    string
	NounName                string
	AuthoredNounName        string
	SpawnKind               uint32
	PoolKind                string
	IsSpawnKindKnown        bool
	Position                Vec3
	Rotation                Vec3
	Scale                   float32
	IsVisible               bool
	IsCollisionEnabled      bool
	TargetMarkerID          uint32
	TeleporterTriggerRadius float32
	NPCProfile              CampaignNPCProfile
	Events                  []CampaignDirectorEvent
}

// CampaignTeleportRoute is one authored one-way tunnel transition and its
// destination placement.
type CampaignTeleportRoute struct {
	MarkerID            uint32
	DestinationMarkerID uint32
	Source              Vec3
	Destination         Vec3
	TriggerRadius       float32
	IsSecurity          bool
	IsBoss              bool
}

// CampaignHordeBarrierSet is one authored horde's projected blocking-door
// objects. HordeGateTeleporter markers remain server-owned contact triggers.
type CampaignHordeBarrierSet struct {
	MarkerSetName string
	Markers       []CampaignDirectorMarker
}

// CampaignDirectorTrigger is one authored player-entry trigger carried as
// immutable setup metadata. It does not publish its named event by itself.
type CampaignDirectorTrigger struct {
	Ordinal  int
	MarkerID uint32
	Name     string
	NounName string
	Position Vec3
	Events   []CampaignDirectorEvent
}

// CampaignDirectorMarkerSet preserves one authored placement-set boundary.
type CampaignDirectorMarkerSet struct {
	Ordinal  int
	Name     string
	Weight   uint32
	Markers  []CampaignDirectorMarker
	Triggers []CampaignDirectorTrigger
}

// CampaignScriptBinding identifies one imported callback attached to an
// authored level event. Runtime operations must still validate any intent
// produced by the referenced script.
type CampaignScriptBinding struct {
	MarkerSetOrdinal      int
	MarkerSetName         string
	MarkerSetWeight       uint32
	MarkerOrdinal         int
	MarkerID              uint32
	MarkerName            string
	NounName              string
	Position              Vec3
	Rotation              Vec3
	Scale                 float32
	IsVisible             bool
	IsCollisionEnabled    bool
	InteractableAbility   string
	InteractableUseLimit  int32
	InteractableChallenge int32
	EventOrdinal          int
	EventName             string
	CallbackName          string
	LuaChunkID            int64
	LuaSourceName         string
	LuaSHA256             string
}

// CampaignScriptObject is one authored marker that has at least one imported
// script callback. Multiple Lua chunks on the same callback do not duplicate
// the replicated object candidate.
type CampaignScriptObject struct {
	MarkerSetOrdinal      int
	MarkerSetName         string
	MarkerSetWeight       uint32
	MarkerOrdinal         int
	MarkerID              uint32
	MarkerName            string
	NounName              string
	Position              Vec3
	Rotation              Vec3
	Scale                 float32
	IsVisible             bool
	IsCollisionEnabled    bool
	InteractableAbility   string
	InteractableUseLimit  int32
	InteractableChallenge int32
	CallbackNames         []string
}

// CampaignDirector is the immutable level input accepted by campaign setup.
type CampaignDirector struct {
	Level                 string
	EntryPositions        []Vec3
	Pools                 []CampaignDirectorPool
	StandaloneBossEntries []CampaignDirectorEntry
	MarkerSets            []CampaignDirectorMarkerSet
	Scripts               []CampaignScriptBinding
	NPCProfilesByNoun     map[string]CampaignNPCProfile
	NPCIdentitiesByNoun   map[string]CampaignNPCIdentity
}

// MarkerObjectIDs returns authored client-owned objects from one marker set
// whose noun matches the requested noun. These IDs are the stable marker IDs
// used by level scenery rather than server-allocated runtime object IDs.
func (d CampaignDirector) MarkerObjectIDs(
	markerSetName string, nounName string,
) []uint32 {
	if markerSetName == "" || nounName == "" {
		return nil
	}
	objectIDs := make([]uint32, 0)
	for _, markerSet := range d.MarkerSets {
		if !strings.EqualFold(markerSet.Name, markerSetName) {
			continue
		}
		for _, marker := range markerSet.Markers {
			if marker.MarkerID == 0 || !strings.EqualFold(marker.NounName, nounName) {
				continue
			}
			objectIDs = append(objectIDs, marker.MarkerID)
		}
		break
	}
	return objectIDs
}

const (
	campaignLootObeliskChallenge   = int32(500)
	campaignHealthObeliskChallenge = int32(100)
	initialChainRegulatorNoun      = "DEST_prefab_islands_instrument_scitech_11.Noun"
)

// TutorialActors returns the fixed non-player placements from Cryos' authored
// AI marker layer. They are world actors rather than random director loci.
func (d CampaignDirector) TutorialActors() ([]CampaignDirectorMarker, error) {
	markers := make([]CampaignDirectorMarker, 0)
	for _, markerSet := range d.MarkerSets {
		if !strings.HasSuffix(strings.ToLower(markerSet.Name), "_ai.markerset") {
			continue
		}
		for _, marker := range markerSet.Markers {
			if !strings.HasPrefix(strings.ToLower(marker.NounName), "tutorial") {
				continue
			}
			if !marker.NPCProfile.IsKnown {
				return nil, fmt.Errorf("tutorialActorProfile[%s]: missing", marker.NounName)
			}
			marker.AuthoredNounName = marker.NounName
			switch strings.ToLower(marker.NounName) {
			case "tutorialbasicpoisonnoorbs.noun":
				marker.NounName = "TutorialBasicPoison.Noun"
			case "tutorialspecialone_intro.noun":
				marker.NounName = "TutorialSpecialOne.Noun"
			}
			markers = append(markers, marker)
		}
	}
	if len(markers) == 0 {
		return nil, errors.New("tutorial actors empty")
	}
	return markers, nil
}

// InitialChainFixtures selects one of 1-1's three equal-weight authored
// Gravitic Regulator variants. These are placed, killable world fixtures, not
// director agents, and therefore must not participate in aggro or clear gates.
func (d CampaignDirector) InitialChainFixtures(matchID uint32) ([]CampaignDirectorMarker, error) {
	if !strings.EqualFold(d.Level, InitialChainLevel) {
		return nil, fmt.Errorf("fixtureLevel: %q", d.Level)
	}
	variantOrdinal := make([]int, 0, 3)
	fixtureByOrdinal := make(map[int][]CampaignDirectorMarker, 3)
	var variantWeight uint32
	for _, markerSet := range d.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		switch name {
		case "zelems_1_smart_objects_1.markerset",
			"zelems_1_smart_objects_2.markerset",
			"zelems_1_smart_objects_3.markerset":
		default:
			continue
		}
		fixtures := make([]CampaignDirectorMarker, 0, 5)
		for _, marker := range markerSet.Markers {
			if strings.EqualFold(marker.NounName, initialChainRegulatorNoun) {
				fixtures = append(fixtures, marker)
			}
		}
		if len(fixtures) == 0 {
			continue
		}
		if markerSet.Weight == 0 || (variantWeight != 0 && markerSet.Weight != variantWeight) {
			return nil, fmt.Errorf("fixtureWeight[%d]: %d", markerSet.Ordinal, markerSet.Weight)
		}
		if len(fixtures) != 5 {
			return nil, fmt.Errorf("fixtureComposition[%d]: %d", markerSet.Ordinal, len(fixtures))
		}
		for markerIndex, marker := range fixtures {
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) ||
				!marker.NPCProfile.IsKnown || marker.NPCProfile.HitPoint <= 0 {
				return nil, fmt.Errorf("fixtureMarker[%d][%d]: invalid", markerSet.Ordinal, markerIndex)
			}
		}
		variantWeight = markerSet.Weight
		variantOrdinal = append(variantOrdinal, markerSet.Ordinal)
		fixtureByOrdinal[markerSet.Ordinal] = fixtures
	}
	if len(variantOrdinal) != 3 {
		return nil, fmt.Errorf("fixtureVariantCount: got %d, want 3", len(variantOrdinal))
	}
	slices.Sort(variantOrdinal)
	selectedOrdinal := variantOrdinal[int(matchID%uint32(len(variantOrdinal)))]
	return slices.Clone(fixtureByOrdinal[selectedOrdinal]), nil
}

// InitialChainFirstClearFixtures materializes the introductory route's fixed
// Gravitic Regulator census. The anchors are the ordered positions captured by
// one complete first-clear 1-1 traversal; replay runs continue to use one of
// the three authored five-object variants selected by InitialChainFixtures.
func (d CampaignDirector) InitialChainFirstClearFixtures() ([]CampaignDirectorMarker, error) {
	templates, err := d.InitialChainFixtures(0)
	if err != nil {
		return nil, fmt.Errorf("firstClearTemplate: %w", err)
	}
	if len(templates) == 0 {
		return nil, errors.New("firstClearTemplate: empty")
	}
	positions := []Vec3{
		{X: -179.441, Y: -82.242, Z: 0.088},
		{X: -183.618, Y: -43.611, Z: 0.088},
		{X: -122.533, Y: -41.325, Z: 0.088},
		{X: -156.339, Y: 56.590, Z: -0.012},
		{X: 572.632, Y: -36.299, Z: 0.088},
		{X: 543.003, Y: 32.458, Z: 5.088},
		{X: 562.072, Y: 23.594, Z: 5.088},
		{X: 595.715, Y: 38.376, Z: 10.088},
		{X: 603.131, Y: -9.330, Z: 10.088},
		{X: 645.209, Y: 1.915, Z: 15.095},
		{X: 633.302, Y: -34.610, Z: 20.088},
		{X: 604.313, Y: -73.930, Z: 25.088},
		{X: 549.380, Y: 25.442, Z: 33.088},
		{X: 524.844, Y: 13.362, Z: 33.088},
		{X: 545.755, Y: 1.496, Z: 33.088},
		{X: 206.631, Y: 727.820, Z: 0.088},
		{X: 211.898, Y: 675.077, Z: 5.088},
		{X: 191.557, Y: 657.295, Z: 5.088},
		{X: 219.908, Y: 587.723, Z: 10.088},
		{X: 255.014, Y: 666.813, Z: 10.088},
		{X: 282.235, Y: 675.438, Z: 10.088},
		{X: 203.116, Y: 628.193, Z: 5.088},
	}
	const firstClearMarkerID = uint32(0xf1100000)
	fixtures := make([]CampaignDirectorMarker, 0, len(positions))
	for positionIndex, position := range positions {
		fixture := templates[positionIndex%len(templates)]
		fixture.Ordinal = positionIndex
		fixture.MarkerID = firstClearMarkerID + uint32(positionIndex) + 1
		fixture.MarkerSetName = "zelems_1_first_clear_regulators.Markerset"
		fixture.Name = fmt.Sprintf("FirstClearGraviticRegulator-%d", positionIndex+1)
		fixture.Position = position
		fixtures = append(fixtures, fixture)
	}
	return fixtures, nil
}

// HordeBarriers returns the exact placed blockers owned by authored horde
// marker sets. Runtime policy owns when they are replicated.
func (d CampaignDirector) HordeBarriers() ([]CampaignHordeBarrierSet, error) {
	barriers := make([]CampaignHordeBarrierSet, 0)
	for _, markerSet := range d.MarkerSets {
		isHordeMarkerSet := false
		for _, trigger := range markerSet.Triggers {
			for _, event := range trigger.Events {
				if event.CallbackName == "HordeTrigger_OnEnterPlayer" &&
					event.EventName == "horde triggered" && event.TriggerRadius > 0 {
					isHordeMarkerSet = true
					break
				}
			}
			if isHordeMarkerSet {
				break
			}
		}
		if !isHordeMarkerSet {
			continue
		}
		selected := make([]CampaignDirectorMarker, 0)
		for _, marker := range markerSet.Markers {
			if strings.EqualFold(marker.NounName, "TestDoor_design_blockin_horde_open.Noun") {
				selected = append(selected, marker)
			}
		}
		if len(selected) == 0 {
			continue
		}
		for markerIndex, marker := range selected {
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) ||
				!isFiniteCampaignPosition(marker.Rotation) || marker.Scale <= 0 ||
				math.IsNaN(float64(marker.Scale)) || math.IsInf(float64(marker.Scale), 0) {
				return nil, fmt.Errorf(
					"barrierMarker[%s][%d]: invalid", markerSet.Name, markerIndex,
				)
			}
		}
		barriers = append(barriers, CampaignHordeBarrierSet{
			MarkerSetName: markerSet.Name, Markers: slices.Clone(selected),
		})
	}
	return barriers, nil
}

// ScriptBindings returns the exact authored scripts attached to a marker
// callback. The returned slice does not alias immutable setup state.
func (d CampaignDirector) ScriptBindings(markerID uint32, callbackName string) []CampaignScriptBinding {
	if markerID == 0 || callbackName == "" {
		return nil
	}
	var bindings []CampaignScriptBinding
	for _, script := range d.Scripts {
		if script.MarkerID != markerID || script.CallbackName != callbackName {
			continue
		}
		bindings = append(bindings, script)
	}
	return bindings
}

// ScriptMarkerCount counts unique authored markers for one callback and Lua
// source. This supplies inputs such as TouchAllObelisks' interactable count
// without treating duplicate script rows as separate objects.
func (d CampaignDirector) ScriptMarkerCount(callbackName, luaSourceName string) int {
	if callbackName == "" || luaSourceName == "" {
		return 0
	}
	markerIDs := make(map[uint32]struct{})
	for _, script := range d.Scripts {
		if script.MarkerID == 0 || script.CallbackName != callbackName ||
			script.LuaSourceName != luaSourceName {
			continue
		}
		markerIDs[script.MarkerID] = struct{}{}
	}
	return len(markerIDs)
}

// ScriptObjects deduplicates script rows into authored object candidates while
// retaining exact callback names. It rejects inconsistent projections of the
// same marker rather than selecting one arbitrarily.
func (d CampaignDirector) ScriptObjects() ([]CampaignScriptObject, error) {
	objects := make([]CampaignScriptObject, 0)
	objectIndexByMarkerID := make(map[uint32]int)
	for scriptIndex, script := range d.Scripts {
		if script.MarkerID == 0 || script.MarkerName == "" || script.NounName == "" ||
			script.CallbackName == "" || !isFiniteCampaignPosition(script.Position) ||
			!isFiniteCampaignPosition(script.Rotation) || script.Scale <= 0 ||
			math.IsNaN(float64(script.Scale)) || math.IsInf(float64(script.Scale), 0) {
			return nil, fmt.Errorf("scriptObject[%d]: invalid", scriptIndex)
		}
		objectIndex, isFound := objectIndexByMarkerID[script.MarkerID]
		if !isFound {
			objects = append(objects, CampaignScriptObject{
				MarkerSetOrdinal: script.MarkerSetOrdinal, MarkerSetName: script.MarkerSetName,
				MarkerSetWeight: script.MarkerSetWeight, MarkerOrdinal: script.MarkerOrdinal,
				MarkerID: script.MarkerID, MarkerName: script.MarkerName, NounName: script.NounName,
				Position: script.Position, Rotation: script.Rotation, Scale: script.Scale,
				IsVisible: script.IsVisible, IsCollisionEnabled: script.IsCollisionEnabled,
				InteractableAbility:   script.InteractableAbility,
				InteractableUseLimit:  script.InteractableUseLimit,
				InteractableChallenge: script.InteractableChallenge,
			})
			objectIndex = len(objects) - 1
			objectIndexByMarkerID[script.MarkerID] = objectIndex
		} else {
			object := objects[objectIndex]
			if object.MarkerSetOrdinal != script.MarkerSetOrdinal ||
				object.MarkerSetName != script.MarkerSetName ||
				object.MarkerSetWeight != script.MarkerSetWeight ||
				object.MarkerOrdinal != script.MarkerOrdinal || object.MarkerName != script.MarkerName ||
				object.NounName != script.NounName || object.Position != script.Position ||
				object.Rotation != script.Rotation || object.Scale != script.Scale ||
				object.IsVisible != script.IsVisible ||
				object.IsCollisionEnabled != script.IsCollisionEnabled ||
				object.InteractableAbility != script.InteractableAbility ||
				object.InteractableUseLimit != script.InteractableUseLimit ||
				object.InteractableChallenge != script.InteractableChallenge {
				return nil, fmt.Errorf("scriptObject[%d]: marker %d conflicts", scriptIndex, script.MarkerID)
			}
		}
		isCallbackFound := false
		for _, callbackName := range objects[objectIndex].CallbackNames {
			if callbackName == script.CallbackName {
				isCallbackFound = true
				break
			}
		}
		if !isCallbackFound {
			objects[objectIndex].CallbackNames = append(
				objects[objectIndex].CallbackNames, script.CallbackName,
			)
		}
	}
	return objects, nil
}

// InitialChainInteractables selects one of 1-1's three equal-weight authored
// obelisk variants. The retail random-source implementation is unavailable, so
// the match ID supplies a stable local selection without materializing all
// mutually exclusive variants.
func (d CampaignDirector) InitialChainInteractables(matchID uint32) ([]CampaignScriptObject, error) {
	if !strings.EqualFold(d.Level, InitialChainLevel) {
		return nil, fmt.Errorf("interactableLevel: %q", d.Level)
	}
	selected, err := d.CampaignInteractables(matchID)
	if err != nil {
		return nil, fmt.Errorf("interactableSelect: %w", err)
	}
	lootCount := 0
	healthCount := 0
	for _, object := range selected {
		switch object.InteractableAbility {
		case "InteractWithObelisk":
			lootCount++
		case "InteractHealthObelisk":
			healthCount++
		}
	}
	if len(selected) != 5 || lootCount != 3 || healthCount != 2 {
		return nil, fmt.Errorf("interactableComposition: objects=%d loot=%d health=%d",
			len(selected), lootCount, healthCount)
	}
	return selected, nil
}

// InitialChainFirstClearInteractables anchors the introductory route's known
// fixed health obelisk while retaining the authored three-loot/two-health
// composition. Replay runs continue to select a weighted authored variant.
func (d CampaignDirector) InitialChainFirstClearInteractables() ([]CampaignScriptObject, error) {
	objects, err := d.InitialChainInteractables(0)
	if err != nil {
		return nil, fmt.Errorf("firstClearInteractableTemplate: %w", err)
	}
	healthIndex := -1
	for objectIndex, object := range objects {
		if object.InteractableAbility == "InteractHealthObelisk" {
			healthIndex = objectIndex
			break
		}
	}
	if healthIndex < 0 {
		return nil, errors.New("firstClearHealthObelisk: missing")
	}
	objects[healthIndex].Position = Vec3{X: 130.303, Y: 629.117, Z: 5.088}
	return objects, nil
}

// CampaignInteractables deterministically selects one authored weighted
// interactable marker-set variant for a match.
func (d CampaignDirector) CampaignInteractables(matchID uint32) ([]CampaignScriptObject, error) {
	objects, err := d.ScriptObjects()
	if err != nil {
		return nil, fmt.Errorf("interactableObjects: %w", err)
	}
	objectsByOrdinal := make(map[int][]CampaignScriptObject)
	weightByOrdinal := make(map[int]uint32)
	for _, object := range objects {
		if object.InteractableAbility == "" {
			continue
		}
		switch object.InteractableAbility {
		case "InteractWithObelisk":
			if object.InteractableChallenge == 0 {
				object.InteractableChallenge = campaignLootObeliskChallenge
			}
		case "InteractHealthObelisk":
			if object.InteractableChallenge == 0 {
				object.InteractableChallenge = campaignHealthObeliskChallenge
			}
		default:
			return nil, fmt.Errorf("interactableAbility[%d]: %q",
				object.MarkerID, object.InteractableAbility)
		}
		if object.MarkerSetWeight == 0 {
			return nil, fmt.Errorf("interactableWeight[%d]: %d",
				object.MarkerSetOrdinal, object.MarkerSetWeight)
		}
		weight, isWeightFound := weightByOrdinal[object.MarkerSetOrdinal]
		if isWeightFound && weight != object.MarkerSetWeight {
			return nil, fmt.Errorf("interactableWeight[%d]: %d != %d",
				object.MarkerSetOrdinal, weight, object.MarkerSetWeight)
		}
		weightByOrdinal[object.MarkerSetOrdinal] = object.MarkerSetWeight
		objectsByOrdinal[object.MarkerSetOrdinal] = append(
			objectsByOrdinal[object.MarkerSetOrdinal], object,
		)
	}
	if len(objectsByOrdinal) == 0 {
		return []CampaignScriptObject{}, nil
	}
	variantOrdinal := make([]int, 0, len(objectsByOrdinal))
	totalWeight := uint64(0)
	commonWeight := uint32(0)
	areWeightsEqual := true
	for markerSetOrdinal, weight := range weightByOrdinal {
		variantOrdinal = append(variantOrdinal, markerSetOrdinal)
		totalWeight += uint64(weight)
		if commonWeight == 0 {
			commonWeight = weight
		} else if commonWeight != weight {
			areWeightsEqual = false
		}
	}
	slices.Sort(variantOrdinal)
	if areWeightsEqual {
		selectedOrdinal := variantOrdinal[int(matchID%uint32(len(variantOrdinal)))]
		return slices.Clone(objectsByOrdinal[selectedOrdinal]), nil
	}
	selection := uint64(matchID) % totalWeight
	selectedOrdinal := 0
	isSelected := false
	for _, markerSetOrdinal := range variantOrdinal {
		weight := uint64(weightByOrdinal[markerSetOrdinal])
		if selection < weight {
			selectedOrdinal = markerSetOrdinal
			isSelected = true
			break
		}
		selection -= weight
	}
	if !isSelected {
		return nil, errors.New("interactableSelection: unavailable")
	}
	return slices.Clone(objectsByOrdinal[selectedOrdinal]), nil
}

// CampaignCallbackObjects deterministically selects one authored weighted
// marker-set variant containing the requested callback. This is used for
// client-presented level objects whose authored callback is client-owned.
func (d CampaignDirector) CampaignCallbackObjects(
	matchID uint32, callbackName string,
) ([]CampaignScriptObject, error) {
	if callbackName == "" {
		return nil, errors.New("callbackObjectName: empty")
	}
	objects, err := d.ScriptObjects()
	if err != nil {
		return nil, fmt.Errorf("callbackObjects: %w", err)
	}
	objectsByOrdinal := make(map[int][]CampaignScriptObject)
	weightsByOrdinal := make(map[int]uint32)
	for _, object := range objects {
		if !slices.Contains(object.CallbackNames, callbackName) {
			continue
		}
		if object.MarkerSetWeight == 0 {
			return nil, fmt.Errorf("callbackObjectWeight[%d]: zero", object.MarkerSetOrdinal)
		}
		weight, isWeightFound := weightsByOrdinal[object.MarkerSetOrdinal]
		if isWeightFound && weight != object.MarkerSetWeight {
			return nil, fmt.Errorf("callbackObjectWeight[%d]: conflict", object.MarkerSetOrdinal)
		}
		weightsByOrdinal[object.MarkerSetOrdinal] = object.MarkerSetWeight
		objectsByOrdinal[object.MarkerSetOrdinal] = append(
			objectsByOrdinal[object.MarkerSetOrdinal], object,
		)
	}
	if len(objectsByOrdinal) == 0 {
		return []CampaignScriptObject{}, nil
	}
	ordinals := make([]int, 0, len(objectsByOrdinal))
	totalWeight := uint64(0)
	for ordinal, weight := range weightsByOrdinal {
		ordinals = append(ordinals, ordinal)
		totalWeight += uint64(weight)
	}
	slices.Sort(ordinals)
	selection := uint64(matchID) % totalWeight
	for _, ordinal := range ordinals {
		weight := uint64(weightsByOrdinal[ordinal])
		if selection < weight {
			return slices.Clone(objectsByOrdinal[ordinal]), nil
		}
		selection -= weight
	}
	return nil, errors.New("callbackObjectSelection: unavailable")
}

// MarkerCount returns the total authored director placements without erasing
// their marker-set ownership.
func (d CampaignDirector) MarkerCount() int {
	count := 0
	for _, markerSet := range d.MarkerSets {
		count += len(markerSet.Markers)
	}
	return count
}

// TriggerCount returns the authored player-entry triggers without treating
// them as director spawn placements.
func (d CampaignDirector) TriggerCount() int {
	count := 0
	for _, markerSet := range d.MarkerSets {
		count += len(markerSet.Triggers)
	}
	return count
}

// TeleportRoutes resolves authored tunnel destinations without assigning any
// server-owned coordinates or traversal order.
func (d CampaignDirector) TeleportRoutes() []CampaignTeleportRoute {
	positionsByMarkerID := make(map[uint32]Vec3)
	for _, markerSet := range d.MarkerSets {
		for _, marker := range markerSet.Markers {
			positionsByMarkerID[marker.MarkerID] = marker.Position
		}
	}
	routes := make([]CampaignTeleportRoute, 0)
	for _, markerSet := range d.MarkerSets {
		for _, marker := range markerSet.Markers {
			isTraversal := strings.EqualFold(marker.NounName, "Teleporter.Noun") ||
				strings.EqualFold(marker.NounName, "TunnelTeleporter.Noun")
			isBoss := strings.EqualFold(marker.NounName, "BossSecurityTeleporter.Noun")
			isSecurity := strings.EqualFold(marker.NounName, "SecurityTeleporter.Noun") || isBoss
			if (!isTraversal && !isSecurity) || marker.TargetMarkerID == 0 {
				continue
			}
			if isSecurity && strings.EqualFold(d.Level, InitialChainLevel) {
				continue
			}
			destination, isFound := positionsByMarkerID[marker.TargetMarkerID]
			if !isFound {
				continue
			}
			routes = append(routes, CampaignTeleportRoute{
				MarkerID: marker.MarkerID, DestinationMarkerID: marker.TargetMarkerID,
				Source:        marker.Position,
				Destination:   destination,
				TriggerRadius: marker.TeleporterTriggerRadius,
				IsSecurity:    isSecurity,
				IsBoss:        isBoss,
			})
		}
	}
	return routes
}

// EligibleEntries returns one pool's entries whose authored inclusive
// difficulty interval contains difficulty. It preserves authored entry order
// and does not apply budget, random selection, or horde-legality policy.
func (d CampaignDirector) EligibleEntries(
	poolKindName string, difficulty uint32,
) ([]CampaignDirectorEntry, error) {
	if poolKindName == "" {
		return nil, errors.New("eligible entries: empty pool kind")
	}
	var matchedPool *CampaignDirectorPool
	for index := range d.Pools {
		if !strings.EqualFold(d.Pools[index].ConfigKind, poolKindName) {
			continue
		}
		if matchedPool != nil {
			return nil, fmt.Errorf("eligible entries: duplicate pool %q", poolKindName)
		}
		matchedPool = &d.Pools[index]
	}
	if matchedPool == nil {
		return nil, fmt.Errorf("eligible entries: pool %q missing", poolKindName)
	}
	entries := make([]CampaignDirectorEntry, 0, len(matchedPool.Entries))
	for _, entry := range matchedPool.Entries {
		if difficulty < entry.MinimumDifficulty || difficulty > entry.MaximumDifficulty {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// CampaignDirectorSource is the content port consumed by campaign setup.
type CampaignDirectorSource interface {
	LoadCampaignDirector(context.Context, string) (CampaignDirector, error)
}

// CampaignSetup validates one authorized campaign binding and loads its
// difficulty-eligible director inputs without choosing or spawning an encounter.
type CampaignSetup struct {
	directorSource CampaignDirectorSource
}

func NewCampaignSetup(directorSource CampaignDirectorSource) (*CampaignSetup, error) {
	if directorSource == nil {
		return nil, errors.New("create campaign setup: nil director source")
	}
	return &CampaignSetup{directorSource: directorSource}, nil
}

func (o *CampaignSetup) Execute(ctx context.Context, binding GameplayBinding) (CampaignDirector, error) {
	if o == nil || o.directorSource == nil {
		return CampaignDirector{}, errors.New("campaign setup: nil operation")
	}
	if ctx == nil {
		return CampaignDirector{}, errors.New("campaign setup: nil context")
	}
	err := ctx.Err()
	if err != nil {
		return CampaignDirector{}, fmt.Errorf("setupContext: %w", err)
	}
	if binding.Mode != ModeChain && binding.Mode != ModeTutorial {
		return CampaignDirector{}, errors.New("campaign setup: invalid mode")
	}
	if binding.Level == "" {
		return CampaignDirector{}, errors.New("campaign setup: empty level")
	}
	director, err := o.directorSource.LoadCampaignDirector(ctx, binding.Level)
	if err != nil {
		return CampaignDirector{}, fmt.Errorf("setupLoad: %w", err)
	}
	if !isCampaignDirectorLevel(binding, director.Level) {
		return CampaignDirector{}, fmt.Errorf("setupLevel: got %q, want %q", director.Level, binding.Level)
	}
	if !binding.IsWarped && len(director.Pools) == 0 {
		return CampaignDirector{}, errors.New("campaign setup: empty director pools")
	}
	if !binding.IsWarped && (len(director.MarkerSets) == 0 || director.MarkerCount() == 0) {
		return CampaignDirector{}, errors.New("campaign setup: empty director markers")
	}
	pools := make([]CampaignDirectorPool, len(director.Pools))
	copy(pools, director.Pools)
	director.Pools = pools
	if !strings.EqualFold(director.Level, InitialChainLevel) {
		for poolIndex := range director.Pools {
			pool := &director.Pools[poolIndex]
			if pool.ConfigKind != "" && !strings.EqualFold(pool.ConfigKind, "unknown") {
				continue
			}
			pool.ConfigKind = campaignDirectorConfigurationKind(pool.ConfigurationOrdinal)
		}
	}
	poolKind := make(map[string]struct{}, len(director.Pools))
	for _, pool := range director.Pools {
		if pool.ConfigKind == "" || strings.EqualFold(pool.ConfigKind, "unknown") {
			continue
		}
		normalizedKind := strings.ToLower(pool.ConfigKind)
		_, isDuplicate := poolKind[normalizedKind]
		if isDuplicate {
			return CampaignDirector{}, fmt.Errorf("campaign setup: duplicate pool %q", pool.ConfigKind)
		}
		poolKind[normalizedKind] = struct{}{}
	}
	for _, markerSet := range director.MarkerSets {
		for _, marker := range markerSet.Markers {
			if !marker.IsSpawnKindKnown {
				continue
			}
			if marker.PoolKind == "" {
				return CampaignDirector{}, fmt.Errorf("campaign setup: marker %d has empty pool kind", marker.MarkerID)
			}
			_, isPoolFound := poolKind[strings.ToLower(marker.PoolKind)]
			if !isPoolFound {
				return CampaignDirector{}, fmt.Errorf("campaign setup: marker %d pool %q missing",
					marker.MarkerID, marker.PoolKind)
			}
		}
	}
	difficulty := binding.Difficulty
	if difficulty == 0 {
		difficulty = MinimumCampaignDifficulty
	}
	if difficulty < MinimumCampaignDifficulty || difficulty > MaximumCampaignDifficulty {
		return CampaignDirector{}, fmt.Errorf("setupDifficulty[%d]: %w", difficulty, ErrGameplayDifficulty)
	}
	for poolIndex := range director.Pools {
		pool := &director.Pools[poolIndex]
		eligibleEntries := make([]CampaignDirectorEntry, 0, len(pool.Entries))
		for _, entry := range pool.Entries {
			if difficulty < entry.MinimumDifficulty || difficulty > entry.MaximumDifficulty {
				continue
			}
			eligibleEntries = append(eligibleEntries, entry)
		}
		pool.Entries = eligibleEntries
	}
	return director, nil
}

func isCampaignDirectorLevel(binding GameplayBinding, directorLevel string) bool {
	if strings.EqualFold(directorLevel, binding.Level) {
		return true
	}
	return binding.Mode == ModeTutorial &&
		strings.EqualFold(binding.Level, TutorialLevel) &&
		strings.EqualFold(directorLevel, TutorialDirectorLevel)
}

func campaignDirectorConfigurationKind(configurationOrdinal int) string {
	switch configurationOrdinal {
	case 0:
		return "minion"
	case 1:
		return "special"
	case 2:
		return "agent"
	case 3:
		return "captain"
	default:
		return "unknown"
	}
}
