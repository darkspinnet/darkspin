package game

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/util"
)

// CampaignDirectorEntry is one difficulty-gated noun candidate from immutable
// level content. It does not assign selection weight or encounter behavior.
type CampaignDirectorEntry struct {
	IsExternalSource          bool
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
	DisplayName           string
	NPCAffixNames         [MaxCampaignNPCAffixCount]string
	NPCAffixModifierNames [MaxCampaignNPCAffixCount]string
	NPCAffixModifierIDs   [MaxCampaignNPCAffixCount]uint32
	IsKnown               bool
}

// CampaignNPCProfile is the transport- and storage-neutral authored
// baseline for one director noun.
type CampaignNPCProfile struct {
	ClassAttributeBases         [22]float32
	AreClassAttributeBasesKnown bool
	BaseHitPoint                float32
	NPCClassInstanceID          uint32
	ActorFootprint              *NavigationFootprint
	AIGraph                     *CampaignAIGraph
	IsClassKnown                bool
	NounType                    NounType
	AggroType                   uint32
	ChallengeValue              int32
	NPCRank                     int32
	NPCType                     uint32
	CreatureType                uint32
	DropTypes                   []uint32
	IsTargetable                bool
	IsPlayerPet                 bool
	PlayerCountHealthScale      float32
	AggroRange                  float32
	AlertRange                  float32
	DropAggroRange              float32
	IdleMovementSpeed           float32
	BaseCombatSpeed             float32
	HitPoint                    float32
	PowerPoint                  float32
	Strength                    float32
	Dexterity                   float32
	Mind                        float32
	DodgeRating                 float32
	ResistRating                float32
	CriticalRating              float32
	GraphicsScale               float32
	FootprintRadius             float32
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
	ConfigurationName    string
	ConfigKind           string
	SpawnKind            string
	Entries              []CampaignDirectorEntry
}

// CampaignDirectorEvent is one authored listener or trigger binding on a
// director marker. Campaign setup carries it without executing it.
type CampaignDirectorEvent struct {
	EventHash          uint32
	NativeCallbackHash uint32
	LuaCallbackName    *string
	Ordinal            int
	ComponentName      string
	EventKind          string
	EventSlot          string
	EventName          string
	CallbackName       string
	TriggerRadius      float32
	IsTriggerOnceOnly  bool
	IsServerOnly       bool
}

// CampaignDirectorMarker is one authored placement owned by a marker set.
type CampaignDirectorMarker struct {
	NounType                NounType
	Interactable            *CampaignInteractableDefinition
	NounInteractable        *CampaignInteractableDefinition
	Combatant               *CampaignCombatantDefinition
	SpawnTrigger            *CampaignSpawnTriggerDefinition
	EventListener           *CampaignEventListenerDefinition
	Ordinal                 int
	MarkerID                uint32
	MarkerSetName           string
	Name                    string
	NounName                string
	AuthoredNounName        string
	SpawnKind               uint32
	PoolKind                string
	IsSpawnKindKnown        bool
	SpawnSectionType        uint32
	IsSpawnSectionKnown     bool
	IsSpikeActive           bool
	Position                Vec3
	Rotation                Vec3
	Scale                   float32
	IsVisible               bool
	IsCollisionEnabled      bool
	TargetMarkerID          uint32
	TeleporterTriggerRadius float32
	SpatialRadius           float32
	ExclusionRadius         float32
	IsExclusionVolume       bool
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
	Rotation            Vec3
	TriggerVolume       *CampaignTriggerVolumeDefinition
	TriggerRadius       float32
	IsSecurity          bool
	IsBoss              bool
	IsTunnel            bool
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
	Rotation          Vec3
	Interactable      *CampaignInteractableDefinition
	Combatant         *CampaignCombatantDefinition
	SpawnTrigger      *CampaignSpawnTriggerDefinition
	EventListener     *CampaignEventListenerDefinition
	ExclusionRadius   float32
	IsExclusionVolume bool
	Ordinal           int
	MarkerID          uint32
	Name              string
	NounName          string
	Position          Vec3
	Events            []CampaignDirectorEvent
}

// CampaignDirectorMarkerSet preserves one authored placement-set boundary.
type CampaignDirectorMarkerSet struct {
	Definitions  []CampaignMarkerDefinition
	Ordinal      int
	Name         string
	CatalogAsset CampaignAssetIdentity
	GroupName    string
	Weight       uint32
	Conditions   []uint32
	Markers      []CampaignDirectorMarker
	Triggers     []CampaignDirectorTrigger
}

// CampaignAssetIdentity records catalog provenance for diagnostics only.
type CampaignAssetIdentity struct {
	Ordinal    *uint32
	AssetName  string
	SourceName string
}

// CampaignScriptBinding identifies one imported callback attached to an
// authored level event. Runtime operations must still validate any intent
// produced by the referenced script.
type CampaignScriptBinding struct {
	Interactable          *CampaignInteractableDefinition
	NounInteractable      *CampaignInteractableDefinition
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
	Interactable          *CampaignInteractableDefinition
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
	AttributeMirrorTuning    AttributeMirrorTuning
	LocalPools               []CampaignDirectorPool
	OrdinaryAgentEntries     []CampaignDirectorEntry
	EliteTunings             []CampaignEliteTuning
	EliteMinionMinimumStage  uint32
	EliteSpecialMinimumStage uint32
	EliteAffixesByNoun       map[string][]CampaignEliteAffix
	// AmbientEliteAffixes is Dark Spin's reconstructed shared promotion pool.
	// Imported class affix intervals remain eligibility evidence, not a native
	// server selection algorithm; ordinary classes have no authored affix list.
	AmbientEliteAffixes           []CampaignEliteAffix
	OwnedSummonProfilesByNoun     map[string]CampaignNPCProfile
	FixtureProfilesByNoun         map[string]CampaignNPCProfile
	EnemyPopulationRules          EnemyPopulationRules
	BossSpawnRules                BossSpawnRules
	selectedDefinitionsByMarkerID map[uint32]CampaignMarkerDefinition
	IsEquipmentDropEnabled        bool
	PickupTuning                  CampaignPickupTuning
	DNADropTuning                 DNADropTuning
	Level                         string
	LevelCatalogAsset             CampaignAssetIdentity
	PlanetConfigName              string
	PrimaryType                   uint32
	SecondaryType                 uint32
	TertiaryType                  uint32
	QuaternaryType                uint32
	Difficulty                    uint32
	CompositionTuning             CampaignCompositionTuning
	OrbDifficultyScales           []float32
	// IsFirstClear retains the requesting player's personal campaign state.
	IsFirstClear bool
	// IsReplay selects the shared randomized replay roster and authored captain.
	IsReplay bool
	// Enabling this mode is server policy, not a recovered original-server decision.
	IsFirstTimeDirectorEnabled bool
	// IsFirstTimeRosterSelected records the party-progress branch after checking
	// that at least one first-time minion qualifies for the full stage number.
	IsFirstTimeRosterSelected bool
	// IsInitialLayoutSelected identifies the map's native placement choices.
	IsInitialLayoutSelected   bool
	CameraYawOverride         float32
	MapVariantSeed            uint32
	EntryPositions            []Vec3
	Pools                     []CampaignDirectorPool
	ExternalPools             []CampaignDirectorPool
	SectionBuckets            []CampaignSectionBucket
	StandaloneBossEntries     []CampaignDirectorEntry
	MarkerSets                []CampaignDirectorMarkerSet
	Scripts                   []CampaignScriptBinding
	NPCProfilesByNoun         map[string]CampaignNPCProfile
	NounFootprintsByNoun      map[uint32]NavigationFootprint
	NounFootprintsByInstance  map[uint32]NavigationFootprint
	NounTypesByInstance       map[uint32]NounType
	NounProjectilesByInstance map[uint32]bool
	StaticBlockersByInstance  map[uint32]bool
	NPCIdentitiesByNoun       map[string]CampaignNPCIdentity
}

// CampaignCompositionTuning supplies authored group-cost settings without
// exposing property hashes or storage encodings to gameplay.
type CampaignCompositionTuning struct {
	GroupChallengeMultiplier float32
}

// CampaignSectionBucket is one source-authored SectionConfig archetype choice.
type CampaignSectionBucket struct {
	Ordinal      int
	Difficulty   int
	MinionCount  int
	SpecialCount int
	Chance       float32
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
	initialChainRegulatorNoun = "DEST_prefab_islands_instrument_scitech_11.Noun"
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

// InitialChainDestructibles retains the fixed 1-1 instruments and one authored
// smart-object layout. Every instrument marker is retired on the client before
// its authoritative, damageable fixture is published.
func (d CampaignDirector) InitialChainDestructibles(selectionID uint32) (
	[]CampaignDirectorMarker, []uint32, error,
) {
	if !strings.EqualFold(d.Level, InitialChainLevel) {
		return nil, nil, fmt.Errorf("fixtureLevel: %q", d.Level)
	}
	if d.IsInitialLayoutSelected {
		markers, deletedObjectIDs, err := d.MapDestructibles()
		if err != nil {
			return nil, nil, fmt.Errorf("fixtureLayout: %w", err)
		}
		return markers, deletedObjectIDs, nil
	}
	selectedSet := fmt.Sprintf("zelems_1_smart_objects_%d.markerset", selectionID%3+1)
	fixtures := make([]CampaignDirectorMarker, 0)
	deletedObjectIDs := make([]uint32, 0)
	seenMarkerIDs := make(map[uint32]bool)
	variantCount := 0
	isDesignFound := false
	isSelectedFound := false
	var variantWeight uint32
	for _, markerSet := range d.MarkerSets {
		name := strings.ToLower(markerSet.Name)
		isSelected := name == selectedSet
		switch name {
		case "zelems_1_design.markerset":
			isDesignFound = true
			isSelected = true
		case "zelems_1_smart_objects_1.markerset",
			"zelems_1_smart_objects_2.markerset",
			"zelems_1_smart_objects_3.markerset":
			variantCount++
			if markerSet.Weight == 0 ||
				(variantWeight != 0 && markerSet.Weight != variantWeight) {
				return nil, nil, fmt.Errorf("fixtureWeight[%d]: %d", markerSet.Ordinal, markerSet.Weight)
			}
			variantWeight = markerSet.Weight
			if isSelected {
				isSelectedFound = true
			}
		default:
			continue
		}
		for _, marker := range markerSet.Markers {
			if !IsGraviticRegulatorNoun(marker.NounName) &&
				!IsGraviticStabilizerNoun(marker.NounName) {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) {
				return nil, nil, fmt.Errorf("fixtureMarker[%d][%d]: invalid", markerSet.Ordinal, marker.Ordinal)
			}
			if !seenMarkerIDs[marker.MarkerID] {
				deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
				seenMarkerIDs[marker.MarkerID] = true
			}
			if !isSelected {
				continue
			}
			if !marker.NPCProfile.IsKnown || !marker.NPCProfile.IsTargetable ||
				marker.NPCProfile.HitPoint <= 0 {
				return nil, nil, fmt.Errorf("fixtureProfile[%d]: unavailable", marker.MarkerID)
			}
			fixtures = append(fixtures, marker)
		}
	}
	if variantCount != 3 || !isDesignFound || !isSelectedFound || len(fixtures) == 0 {
		return nil, nil, fmt.Errorf("fixtureComposition: design=%t selected=%t variants=%d fixtures=%d",
			isDesignFound, isSelectedFound, variantCount, len(fixtures))
	}
	return fixtures, deletedObjectIDs, nil
}

func (d CampaignDirector) InitialChainFixtures(matchID uint32) ([]CampaignDirectorMarker, error) {
	fixtures, deletedObjectIDs, err := d.InitialChainDestructibles(matchID)
	if err != nil {
		return nil, fmt.Errorf("initialFixtures: %w", err)
	}
	if len(deletedObjectIDs) == 0 {
		return nil, errors.New("initialFixtures: no authored objects")
	}
	return fixtures, nil
}

// First clear uses the first authored object layout, as its other content does.
func (d CampaignDirector) InitialChainFirstClearFixtures() ([]CampaignDirectorMarker, error) {
	fixtures, err := d.InitialChainFixtures(0)
	if err != nil {
		return nil, fmt.Errorf("firstClearFixtures: %w", err)
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
		interactable, err := resolveCampaignInteractable(script.Interactable, script.NounInteractable)
		if err != nil {
			return nil, fmt.Errorf("scriptInteractable[%d]: %w", scriptIndex, err)
		}
		if interactable != nil {
			script.InteractableAbility = ""
			if interactable.AbilityName != nil {
				script.InteractableAbility = *interactable.AbilityName
			}
			script.InteractableUseLimit = interactable.UseLimit
			script.InteractableChallenge = interactable.Challenge
		}
		if script.MarkerID == 0 || script.MarkerName == "" || script.NounName == "" ||
			script.CallbackName == "" || !isFiniteCampaignPosition(script.Position) ||
			!isFiniteCampaignPosition(script.Rotation) || script.Scale <= 0 ||
			math.IsNaN(float64(script.Scale)) || math.IsInf(float64(script.Scale), 0) {
			return nil, fmt.Errorf("scriptObject[%d]: invalid", scriptIndex)
		}
		objectIndex, isFound := objectIndexByMarkerID[script.MarkerID]
		if !isFound {
			objects = append(objects, CampaignScriptObject{
				Interactable:     interactable,
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

// InitialChainInteractables preserves the seeded layout's authored obelisks,
// including empty alternatives. Legacy match-ID selection retains its fixed
// three-loot/two-health composition check.
func (d CampaignDirector) InitialChainInteractables(matchID uint32) ([]CampaignScriptObject, error) {
	if !strings.EqualFold(d.Level, InitialChainLevel) {
		return nil, fmt.Errorf("interactableLevel: %q", d.Level)
	}
	selected, err := d.CampaignInteractables(matchID)
	if err != nil {
		return nil, fmt.Errorf("interactableSelect: %w", err)
	}
	if d.IsInitialLayoutSelected {
		return selected, nil
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

// CampaignInteractables uses the selected layout when available. Legacy
// callers retain an independently weighted interactable variant for a match.
func (d CampaignDirector) CampaignInteractables(matchID uint32) ([]CampaignScriptObject, error) {
	objects, err := d.obeliskObjects()
	if err != nil {
		return nil, fmt.Errorf("interactableObjects: %w", err)
	}
	objectsByOrdinal := make(map[int][]CampaignScriptObject)
	weightByOrdinal := make(map[int]uint32)
	selectedObjects := make([]CampaignScriptObject, 0)
	for _, object := range objects {
		if object.InteractableAbility == "" {
			continue
		}
		switch object.InteractableAbility {
		case "InteractWithObelisk", "InteractHealthObelisk":
		default:
			return nil, fmt.Errorf("interactableAbility[%d]: %q",
				object.MarkerID, object.InteractableAbility)
		}
		if d.IsInitialLayoutSelected {
			selectedObjects = append(selectedObjects, object)
			continue
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
	if d.IsInitialLayoutSelected {
		return selectedObjects, nil
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

// CampaignCallbackObjects preserves callbacks from the selected map layout.
// Legacy callers without a selection retain one weighted callback variant.
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
	if d.IsInitialLayoutSelected {
		selectedObjects := make([]CampaignScriptObject, 0)
		for _, object := range objects {
			if slices.Contains(object.CallbackNames, callbackName) {
				selectedObjects = append(selectedObjects, object)
			}
		}
		return selectedObjects, nil
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

// TeleportRoutes uses selected definitions for both endpoints. Route eligibility
// remains separate from lookup membership: invisible and non-director markers
// can still be destinations, and absent selected destinations produce no route.
func (e CampaignDirector) TeleportRoutes() []CampaignTeleportRoute {
	routes := make([]CampaignTeleportRoute, 0)
	resolvedMarkerIDs := make(map[uint32]struct{})
	for _, set := range e.MarkerSets {
		for _, definition := range set.Definitions {
			if _, isResolved := resolvedMarkerIDs[definition.MarkerID]; isResolved {
				continue
			}
			resolvedMarkerIDs[definition.MarkerID] = struct{}{}
			marker, isFound := e.SelectedMarkerDefinition(definition.MarkerID)
			if !isFound || marker.Teleporter == nil {
				continue
			}
			isTraversal := marker.Teleporter.isTraversalCallback() ||
				strings.EqualFold(marker.NounName, "Teleporter.Noun") ||
				strings.EqualFold(marker.NounName, "TunnelTeleporter.Noun")
			isBoss := strings.EqualFold(marker.NounName, "BossSecurityTeleporter.Noun")
			isSecurity := strings.EqualFold(marker.NounName, "SecurityTeleporter.Noun") || isBoss
			if !isTraversal && !isSecurity {
				continue
			}
			if isSecurity && strings.EqualFold(e.Level, InitialChainLevel) {
				continue
			}
			destination, isFound := e.TeleportDestination(marker.MarkerID)
			if !isFound {
				continue
			}
			routes = append(routes, CampaignTeleportRoute{
				MarkerID: marker.MarkerID, DestinationMarkerID: marker.Teleporter.DestinationMarkerID,
				Source: marker.Position, Destination: destination,
				Rotation: marker.Rotation, TriggerVolume: marker.Teleporter.TriggerVolume,
				TriggerRadius: marker.TeleporterTriggerRadius, IsSecurity: isSecurity, IsBoss: isBoss,
				IsTunnel: strings.EqualFold(marker.NounName, "TunnelTeleporter.Noun") ||
					(marker.Teleporter.TriggerVolume != nil &&
						marker.Teleporter.TriggerVolume.OnEnterHash == util.HashID("TunnelTeleporter_OnEnter")),
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
		pool := &d.Pools[index]
		if strings.EqualFold(pool.ConfigurationName, "firstTimeConfig") {
			if d.IsFirstTimeRosterSelected {
				matchedPool = pool
				break
			}
			continue
		}
		if matchedPool == nil || strings.EqualFold(pool.ConfigurationName, "levelConfig") {
			matchedPool = pool
		}
	}
	if d.IsFirstTimeRosterSelected &&
		(strings.EqualFold(poolKindName, "minion") || strings.EqualFold(poolKindName, "special")) &&
		(matchedPool == nil || !strings.EqualFold(matchedPool.ConfigurationName, "firstTimeConfig")) {
		return nil, nil
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
	LoadCampaignDirector(context.Context, string, uint32) (CampaignDirector, error)
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
	difficulty := binding.Difficulty
	if difficulty == 0 {
		difficulty = MinimumCampaignDifficulty
	}
	if difficulty < MinimumCampaignDifficulty || difficulty > MaximumCampaignDifficulty {
		return CampaignDirector{}, fmt.Errorf("setupDifficulty[%d]: %w", difficulty, ErrGameplayDifficulty)
	}
	director, err := o.directorSource.LoadCampaignDirector(ctx, binding.Level, difficulty)
	if err != nil {
		return CampaignDirector{}, fmt.Errorf("setupLoad: %w", err)
	}
	if !isCampaignDirectorLevel(binding, director.Level) {
		return CampaignDirector{}, fmt.Errorf("setupLevel: got %q, want %q", director.Level, binding.Level)
	}
	director.IsFirstClear = binding.IsFirstRunLevel()
	director.IsReplay = binding.IsReplayRandomized
	director.IsFirstTimeDirectorEnabled = binding.IsFirstTimeDirectorEnabled
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
		normalizedKind := strings.ToLower(pool.ConfigurationName + ":" + pool.ConfigKind)
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
			_, isPoolFound := poolKind[strings.ToLower("levelConfig:"+marker.PoolKind)]
			if !isPoolFound {
				_, isPoolFound = poolKind[strings.ToLower("firstTimeConfig:"+marker.PoolKind)]
			}
			if !isPoolFound {
				_, isPoolFound = poolKind[strings.ToLower(":"+marker.PoolKind)]
			}
			// Horde and boss-add markers use the mission's minion roster
			// when no separate agent pool is authored, beyond 1-1 as well.
			if !isPoolFound && strings.EqualFold(marker.PoolKind, "agent") {
				_, isPoolFound = poolKind["firsttimeconfig:minion"]
				if !isPoolFound {
					_, isPoolFound = poolKind["levelconfig:minion"]
				}
				if !isPoolFound {
					_, isPoolFound = poolKind[":minion"]
				}
			}
			if !isPoolFound {
				return CampaignDirector{}, fmt.Errorf("campaign setup: marker %d pool %q missing",
					marker.MarkerID, marker.PoolKind)
			}
		}
	}
	director.Difficulty = difficulty
	chapter := (difficulty-1)/4 + 1
	selectedBuckets := make([]CampaignSectionBucket, 0, len(director.SectionBuckets))
	for _, bucket := range director.SectionBuckets {
		if uint32(bucket.Difficulty) == chapter {
			selectedBuckets = append(selectedBuckets, bucket)
		}
	}
	director.SectionBuckets = selectedBuckets
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
	if !binding.IsWarped {
		director.LocalPools = slices.Clone(director.Pools)
		director, err = composeCampaignRoster(director, binding.RunSeed, binding.PartyCompletedStages)
		if err != nil {
			return CampaignDirector{}, fmt.Errorf("setupRoster: %w", err)
		}
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
