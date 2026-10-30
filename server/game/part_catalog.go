package game

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

const partAttributeCount = 115

const (
	campaignRarityStream   uint32 = 0
	campaignRigblockStream uint32 = 1
	campaignSlotStream     uint32 = 5
)

const (
	campaignPlanetCount            = uint32(4)
	campaignMajorItemLevelScale    = uint32(10)
	campaignBoundaryItemLevelBonus = uint32(6)
)

var campaignPartSlotTypes = [...]string{
	"weapon", "grasper", "foot", "defense", "offense", "utility",
}

const campaignRarityPityMaximumMisses = uint32(20)

var campaignRarityPityScales = [...]float64{0, 0.05, 0.10, 0.20}

// CampaignPartSlotBag prevents a campaign player from repeatedly drawing one
// equipment category while other compatible categories remain available.
type CampaignPartSlotBag struct {
	usedSlotTypes map[string]struct{}
	lastSlotType  string
}

// CampaignPartRarityBag progressively weights an authored rarity when that
// exact rarity has not appeared among a player's recent campaign equipment.
type CampaignPartRarityBag struct {
	dryDrawCounts      [4]uint32
	IsDestructorReward bool
}

// Clone creates an independent bag so callers can commit a draw only after the
// corresponding world pickup has been registered successfully.
func (e CampaignPartSlotBag) Clone() CampaignPartSlotBag {
	clone := CampaignPartSlotBag{lastSlotType: e.lastSlotType}
	if len(e.usedSlotTypes) == 0 {
		return clone
	}
	clone.usedSlotTypes = make(map[string]struct{}, len(e.usedSlotTypes))
	for slotType := range e.usedSlotTypes {
		clone.usedSlotTypes[slotType] = struct{}{}
	}
	return clone
}

// Clone creates an independent rarity bag for transactional drop generation.
func (e CampaignPartRarityBag) Clone() CampaignPartRarityBag {
	return e
}

// PartDefinition contains profile-facing base-item metadata proven by content.
type PartDefinition struct {
	RigblockID      uint16
	CatalogOrdinal  *uint32
	ContentFlags    uint8
	MinimumLevel    uint32
	MaximumLevel    uint32
	IsUniqueFamily  bool
	SlotType        string
	ClassType       string
	ScienceType     string
	WeaponSlotType  string
	WeaponOwnerName string
}

// PartAffixDefinition contains one immutable prefix or suffix input vector.
type PartAffixDefinition struct {
	PartTypes           []string
	ModifierGrants      []string
	AbilityImprovements []string
	Kind                string
	ID                  uint16
	CatalogOrdinal      *uint32
	MinimumLevel        uint32
	MaximumLevel        uint32
	ClassType           string
	ScienceType         string
	Modifiers           [partAttributeCount]float32
	IsUniqueFamily      bool
	IsBasicEligible     bool
}

// PartLevelBand applies one exponential base through an inclusive item level.
type PartLevelBand struct {
	Base         float32
	MaximumLevel uint32
}

// PartRarityDistribution scales the four native item modifier blocks.
type PartRarityDistribution struct {
	Suffix   float32
	Prefix   float32
	Prefix2  float32
	Standard float32
}

// PartTuning contains the native operands used to derive display stats.
type PartTuning struct {
	BasePoint              float32
	ExtraStatBonusFactor   float32
	LevelBands             []PartLevelBand
	PointCosts             [13]uint32
	RarityDistributions    [4]PartRarityDistribution
	HandLevelScale         float32
	HandFlat               float32
	FootLevelScale         float32
	FootFlat               float32
	DefenseFlat            float32
	OffenseFlat            float32
	UtilityFlat            float32
	WeaponDamageMultiplier float32
	PriceBase              float32
	PriceCurve             float32
	PriceIncrement         uint32
	RarityLevelStep        uint32
	MajorLevelMultiplier   uint32
	MinorStageCount        uint32
	MinorLevelMultiplier   uint32
	LastMinorLevelBonus    uint32
	RarityLevelMultiplier  uint32
	SidekickProgressionGap int32
	IsSidekickingForced    bool
	HandMinimumLevel       uint32
	FootMinimumLevel       uint32
	WeaponMinimumLevel     uint32
	SlotMinimumStages      map[string]uint32
	UncommonChances        [19]float32
	RareChances            [19]float32
	EpicChances            [19]float32
}

// CriticalAttributes is the equipped-item contribution to critical combat.
type CriticalAttributes struct {
	Rating         float32
	AutoCrit       float32
	DamageIncrease float32
}

// PartCatalog indexes immutable base-item definitions by rigblock ID.
type PartCatalog struct {
	rigblocks       []PartDefinition
	partsByRigblock map[uint16]PartDefinition
	affixesByName   map[string]PartAffixDefinition
	affixesByKind   map[string][]PartAffixDefinition
	tuning          *PartTuning
	isArena         bool
}

func NewPartCatalog(definitions []PartDefinition) (*PartCatalog, error) {
	return NewPartStatCatalog(definitions, nil, nil)
}

// NewPartStatCatalog indexes base items, affixes, and their immutable tuning.
func NewPartStatCatalog(definitions []PartDefinition, affixes []PartAffixDefinition, tuning *PartTuning) (*PartCatalog, error) {
	catalog := &PartCatalog{
		rigblocks:       append([]PartDefinition(nil), definitions...),
		partsByRigblock: make(map[uint16]PartDefinition, len(definitions)),
		affixesByName:   make(map[string]PartAffixDefinition, len(affixes)),
		affixesByKind:   make(map[string][]PartAffixDefinition, 2),
		tuning:          tuning,
	}
	slices.SortStableFunc(catalog.rigblocks, func(first, second PartDefinition) int {
		return compareCatalogOrdinal(first.CatalogOrdinal, second.CatalogOrdinal)
	})
	for index, definition := range catalog.rigblocks {
		if definition.RigblockID == 0 {
			return nil, fmt.Errorf("definitionID[%d]: zero", index)
		}
		if definition.SlotType == "" || definition.ClassType == "" || definition.ScienceType == "" {
			return nil, fmt.Errorf("definitionFields[%d]: incomplete", index)
		}
		if definition.SlotType == "weapon" {
			if definition.WeaponSlotType != "" && definition.WeaponSlotType != "grasper" &&
				definition.WeaponSlotType != "foot" {
				return nil, fmt.Errorf("definitionWeaponSlot[%d]: %s", index, definition.WeaponSlotType)
			}
		}
		if _, isFound := catalog.partsByRigblock[definition.RigblockID]; isFound {
			return nil, fmt.Errorf("definitionDuplicate[%d]: %d", index, definition.RigblockID)
		}
		catalog.partsByRigblock[definition.RigblockID] = definition
	}
	for index, affix := range affixes {
		if affix.ID == 0 || (affix.Kind != "prefix" && affix.Kind != "suffix") {
			return nil, fmt.Errorf("affixIdentity[%d]: %s/%d", index, affix.Kind, affix.ID)
		}
		if affix.MinimumLevel == 0 || affix.MinimumLevel > affix.MaximumLevel {
			return nil, fmt.Errorf("affixEligibility[%d]: %s/%d", index, affix.Kind, affix.ID)
		}
		// Authored affixes can have empty eligibility lists. Preserve them for
		// identity and stat lookup; candidate selection checks list membership.
		affix.PartTypes = append([]string(nil), affix.PartTypes...)
		affix.ModifierGrants = append([]string(nil), affix.ModifierGrants...)
		affix.AbilityImprovements = append([]string(nil), affix.AbilityImprovements...)
		key := partAffixKey(affix.Kind, affix.ID)
		if _, isFound := catalog.affixesByName[key]; isFound {
			return nil, fmt.Errorf("affixDuplicate[%d]: %s", index, key)
		}
		catalog.affixesByName[key] = affix
		catalog.affixesByKind[affix.Kind] = append(catalog.affixesByKind[affix.Kind], affix)
	}
	for kind := range catalog.affixesByKind {
		slices.SortStableFunc(catalog.affixesByKind[kind], func(first, second PartAffixDefinition) int {
			return compareCatalogOrdinal(first.CatalogOrdinal, second.CatalogOrdinal)
		})
	}
	if tuning != nil {
		if tuning.BasePoint <= 0 || tuning.ExtraStatBonusFactor < 0 || len(tuning.LevelBands) == 0 ||
			tuning.WeaponDamageMultiplier <= 0 || tuning.MajorLevelMultiplier == 0 ||
			tuning.MinorStageCount == 0 {
			return nil, fmt.Errorf("tuningInvalid: %#v", *tuning)
		}
	}
	return catalog, nil
}

func compareCatalogOrdinal(first *uint32, second *uint32) int {
	if first == nil && second == nil {
		return 0
	}
	if first == nil {
		return 1
	}
	if second == nil {
		return -1
	}
	if *first < *second {
		return -1
	}
	if *first > *second {
		return 1
	}
	return 0
}

func (c *PartCatalog) ByRigblock(rigblockID uint16) (PartDefinition, bool) {
	if c == nil {
		return PartDefinition{}, false
	}
	definition, isFound := c.partsByRigblock[rigblockID]
	return definition, isFound
}

// GenerateCampaignPart chooses one in-level campaign drop for any hero
// using the ordinary rarity distribution. The caller owns random-stream
// selection and durable identity. Campaign stage gates categories independently
// of the generated item level and recipient progression.
func (c *PartCatalog) GenerateCampaignPart(
	classType string, scienceType string, difficulty uint32, campaignStage uint32, choice uint32, isBoss bool,
) (sporenet.Part, error) {
	rarity := c.campaignPartRarity(
		difficulty, campaignPartChoice(choice, campaignRarityStream),
	)
	dropLevel := c.CampaignItemLevel(campaignStage, rarity, isBoss, 0)
	return c.generateCampaignPart(
		classType, scienceType, "", dropLevel, campaignStage, choice, rarity, "",
	)
}

// GenerateCampaignCreaturePart retains the caller's hero context without
// restricting the in-level item pool to that hero or its squad.
func (e *PartCatalog) GenerateCampaignCreaturePart(
	classType string, scienceType string, creatureName string,
	difficulty uint32, campaignStage uint32, choice uint32, isBoss bool,
) (sporenet.Part, error) {
	rarity := e.campaignPartRarity(
		difficulty, campaignPartChoice(choice, campaignRarityStream),
	)
	dropLevel := e.CampaignItemLevel(campaignStage, rarity, isBoss, 0)
	return e.generateCampaignPart(
		classType, scienceType, creatureName, dropLevel, campaignStage, choice, rarity, "",
	)
}

// GenerateCampaignPartFromBag applies player-local category rotation and
// rarity pity while retaining the ordinary campaign item policy.
func (e *PartCatalog) GenerateCampaignPartFromBag(
	classType string, scienceType string, creatureName string,
	difficulty uint32, campaignStage uint32,
	choice uint32, slotBag *CampaignPartSlotBag, rarityBag *CampaignPartRarityBag, isBoss bool,
) (sporenet.Part, error) {
	if slotBag == nil && rarityBag == nil {
		part, err := e.GenerateCampaignCreaturePart(
			classType, scienceType, creatureName, difficulty, campaignStage, choice, isBoss,
		)
		if err != nil {
			return sporenet.Part{}, fmt.Errorf("bagFallback: %w", err)
		}
		return part, nil
	}
	rarity := e.campaignPartRarityFromBag(
		difficulty, campaignPartChoice(choice, campaignRarityStream),
		rarityBag,
	)
	dropLevel := e.CampaignItemLevel(campaignStage, rarity, isBoss, 0)
	availableSlotTypes := e.campaignPartSlotTypes(
		classType, scienceType, creatureName, campaignStage,
		isCampaignUniqueRarity(rarity), dropLevel,
	)
	if len(availableSlotTypes) == 0 {
		return sporenet.Part{}, errors.New("campaign part slot eligibility empty")
	}
	slotChoice := campaignPartChoice(choice, campaignSlotStream)
	slotType := e.campaignPartSlotType(
		classType, scienceType, creatureName, campaignStage, choice,
		isCampaignUniqueRarity(rarity), dropLevel,
	)
	isRefill := false
	if slotBag != nil {
		slotType, isRefill = slotBag.selectSlotType(availableSlotTypes, slotChoice)
	}
	part, err := e.generateCampaignPart(
		classType, scienceType, creatureName, dropLevel, campaignStage, choice,
		rarity, slotType,
	)
	if err != nil {
		return sporenet.Part{}, fmt.Errorf("bagGenerate: %w", err)
	}
	if slotBag != nil {
		slotBag.recordSlotType(slotType, isRefill)
	}
	if rarityBag != nil {
		rarityBag.recordRarity(rarity)
	}
	return part, nil
}

// GenerateCampaignPartForSlot chooses an ordinary campaign drop from one
// equipment slot while retaining the normal rarity, level, and affix policy.
func (c *PartCatalog) GenerateCampaignPartForSlot(
	classType string, scienceType string, difficulty uint32, campaignStage uint32,
	choice uint32, slotType string, isBoss bool,
) (sporenet.Part, error) {
	if !isCampaignPartSlotType(slotType) {
		return sporenet.Part{}, errors.New("campaign part slot invalid")
	}
	rarity := c.campaignPartRarity(
		difficulty, campaignPartChoice(choice, campaignRarityStream),
	)
	dropLevel := c.CampaignItemLevel(campaignStage, rarity, isBoss, 0)
	return c.generateCampaignPart(
		classType, scienceType, "", dropLevel, campaignStage, choice, rarity, slotType,
	)
}

// GenerateCampaignCreaturePartForSlot applies an explicit developer slot while
// allowing in-level items for every hero.
func (e *PartCatalog) GenerateCampaignCreaturePartForSlot(
	classType string, scienceType string, creatureName string,
	difficulty uint32, campaignStage uint32, choice uint32, slotType string, isBoss bool,
) (sporenet.Part, error) {
	if !isCampaignPartSlotType(slotType) {
		return sporenet.Part{}, errors.New("campaign part slot invalid")
	}
	rarity := e.campaignPartRarity(
		difficulty, campaignPartChoice(choice, campaignRarityStream),
	)
	dropLevel := e.CampaignItemLevel(campaignStage, rarity, isBoss, 0)
	return e.generateCampaignPart(
		classType, scienceType, creatureName, dropLevel, campaignStage, choice,
		rarity, slotType,
	)
}

// GenerateCampaignSpecialPart applies ordinary campaign level, rarity, and
// affix budgets to a specific base item. This lets promotional
// bases participate in gameplay drops without inheriting their entitlement
// sentinel level or bypassing normal item-power limits.
func (c *PartCatalog) GenerateCampaignSpecialPart(
	classType string, scienceType string, difficulty uint32, campaignStage uint32,
	choice uint32, rigblockID uint16, isBoss bool,
) (sporenet.Part, error) {
	rarity := c.campaignPartRarity(
		difficulty, campaignPartChoice(choice, campaignRarityStream),
	)
	return c.generateCampaignSpecialPart(
		classType, scienceType, "", difficulty, campaignStage, choice, rigblockID, rarity, isBoss,
	)
}

// GenerateCampaignSpecialPartFromBag applies player-local rarity pity to a
// limited-edition campaign base while retaining its item budget.
func (e *PartCatalog) GenerateCampaignSpecialPartFromBag(
	classType string, scienceType string, creatureName string,
	difficulty uint32, campaignStage uint32,
	choice uint32, rigblockID uint16, rarityBag *CampaignPartRarityBag, isBoss bool,
) (sporenet.Part, error) {
	if rarityBag == nil {
		part, err := e.GenerateCampaignSpecialPart(
			classType, scienceType, difficulty, campaignStage, choice, rigblockID, isBoss,
		)
		if err != nil {
			return sporenet.Part{}, fmt.Errorf("specialBagFallback: %w", err)
		}
		return part, nil
	}
	rarity := e.campaignPartRarityFromBag(
		difficulty, campaignPartChoice(choice, campaignRarityStream), rarityBag,
	)
	part, err := e.generateCampaignSpecialPart(
		classType, scienceType, creatureName, difficulty, campaignStage, choice,
		rigblockID, rarity, isBoss,
	)
	if err != nil {
		return sporenet.Part{}, fmt.Errorf("specialBagGenerate: %w", err)
	}
	rarityBag.recordRarity(rarity)
	return part, nil
}

func (e *PartCatalog) generateCampaignSpecialPart(
	classType string, scienceType string, creatureName string,
	difficulty uint32, campaignStage uint32,
	choice uint32, rigblockID uint16, rarity sporenet.PartRarity, isBoss bool,
) (sporenet.Part, error) {
	if e == nil || difficulty == 0 {
		return sporenet.Part{}, errors.New("campaign special part unavailable")
	}
	definition, isFound := e.ByRigblock(rigblockID)
	if !isFound || !e.isPartSlotUnlocked(definition, campaignStage) {
		return sporenet.Part{}, errors.New("campaign special part incompatible")
	}
	dropLevel := e.CampaignItemLevel(campaignStage, rarity, isBoss, 0)
	part := sporenet.NewPart(rigblockID)
	if part.RigblockAssetID != rigblockID {
		return sporenet.Part{}, errors.New("campaign special rigblock invalid")
	}
	part.Level = uint16(max(uint32(1), min(dropLevel, uint32(^uint16(0)))))
	part.Rarity = rarity
	err := e.rollBudgetedCampaignAffixes(&part, choice)
	if err != nil {
		return sporenet.Part{}, fmt.Errorf("specialAffixes: %w", err)
	}
	err = e.ValidateGeneratedCampaignPart(part)
	if err != nil {
		return sporenet.Part{}, fmt.Errorf("specialComplete: %w", err)
	}
	part.Cost = e.partCost(part.Level)
	return part, nil
}

// CampaignItemLevel applies authored LootPreferences operands. Boss context
// adds its own bonus; only unique rarities receive the fourth-minor bonus.
// World drops pass chainArgument=0. The optional chain adjustment is independent.
func (e *PartCatalog) CampaignItemLevel(
	stage uint32, rarity sporenet.PartRarity, isBoss bool, chainArgument uint32,
) uint32 {
	if stage == 0 {
		return 0
	}
	minorStageCount := campaignPlanetCount
	majorMultiplier := campaignMajorItemLevelScale
	minorMultiplier := uint32(1)
	boundaryBonus := campaignBoundaryItemLevelBonus
	rarityMultiplier := uint32(5)
	if e != nil && e.tuning != nil {
		minorStageCount = e.tuning.MinorStageCount
		majorMultiplier = e.tuning.MajorLevelMultiplier
		minorMultiplier = e.tuning.MinorLevelMultiplier
		boundaryBonus = e.tuning.LastMinorLevelBonus
		rarityMultiplier = e.tuning.RarityLevelMultiplier
	}
	if minorStageCount == 0 {
		return 0
	}
	major := (stage-1)/minorStageCount + 1
	minor := (stage-1)%minorStageCount + 1
	majorLevel := uint64(major) * uint64(majorMultiplier)
	minorLevel := uint64(minor) * uint64(minorMultiplier)
	if majorLevel > math.MaxUint32 || minorLevel > math.MaxUint32-majorLevel {
		return math.MaxUint32
	}
	rarityOffset := int64(rarity) - 1
	if rarity >= sporenet.PartUnique {
		rarityOffset = int64(rarity) - int64(sporenet.PartUnique)
	}
	level := int64(majorLevel+minorLevel) + int64(rarityMultiplier)*rarityOffset
	if isBoss {
		level += int64(boundaryBonus)
	}
	if rarity >= sporenet.PartUnique && minor == minorStageCount {
		level += int64(boundaryBonus)
	}
	if chainArgument > 1 && minor != minorStageCount {
		level += int64(min(chainArgument-1, uint32(3)))
	}
	return uint32(max(int64(1), min(level, int64(math.MaxUint32))))
}

// GenerateCampaignRewardPart chooses a cash-out item in the unique rarity
// family selected by the result roll band.
func (c *PartCatalog) GenerateCampaignRewardPart(
	classType string, scienceType string, chainArgument uint32, campaignStage uint32, choice uint32,
	rarity sporenet.PartRarity,
) (sporenet.Part, error) {
	if rarity < sporenet.PartUnique || rarity > sporenet.PartEpicUnique {
		return sporenet.Part{}, errors.New("campaign part rarity invalid")
	}
	level := c.CampaignItemLevel(campaignStage, rarity, false, chainArgument)
	return c.generateCampaignPart(
		classType, scienceType, "", level, campaignStage, choice, rarity, "",
	)
}

// GenerateCampaignCreatureRewardPart allows cash-out items for every
// hero family while retaining the chosen unique rarity band.
func (e *PartCatalog) GenerateCampaignCreatureRewardPart(
	classType string, scienceType string, creatureName string,
	chainArgument uint32, campaignStage uint32, choice uint32, rarity sporenet.PartRarity,
) (sporenet.Part, error) {
	if rarity < sporenet.PartUnique || rarity > sporenet.PartEpicUnique {
		return sporenet.Part{}, errors.New("campaign part rarity invalid")
	}
	level := e.CampaignItemLevel(campaignStage, rarity, false, chainArgument)
	return e.generateCampaignPart(
		classType, scienceType, creatureName, level, campaignStage, choice, rarity, "",
	)
}

func (c *PartCatalog) generateCampaignPart(
	classType string, scienceType string, creatureName string,
	level uint32, campaignStage uint32, choice uint32,
	rarity sporenet.PartRarity, slotType string,
) (sporenet.Part, error) {
	if c == nil {
		return sporenet.Part{}, errors.New("campaign part catalog unavailable")
	}
	isUniqueFamily := isCampaignUniqueRarity(rarity)
	if slotType == "" {
		slotType = c.campaignPartSlotType(
			classType, scienceType, creatureName, campaignStage, choice, isUniqueFamily, level,
		)
		if slotType == "" {
			return sporenet.Part{}, errors.New("campaign part slot eligibility empty")
		}
	}
	eligibleIDs := make([]uint16, 0, len(c.partsByRigblock))
	slotMask := partSlotBit(slotType)
	scienceMask := campaignLootScienceMask
	for _, definition := range c.rigblocks {
		rigblockID := definition.RigblockID
		if !isCampaignRigblockEligible(
			definition, slotMask, scienceMask, level, isUniqueFamily,
		) ||
			!c.isPartSlotUnlocked(definition, campaignStage) {
			continue
		}
		eligibleIDs = append(eligibleIDs, rigblockID)
	}
	if len(eligibleIDs) == 0 {
		return sporenet.Part{}, errors.New("campaign rigblock pool empty")
	}
	rigblockChoice := campaignPartChoice(choice, campaignRigblockStream)
	index := int(rigblockChoice % uint32(len(eligibleIDs)))
	part := sporenet.NewPart(eligibleIDs[index])
	if part.RigblockAssetID != eligibleIDs[index] {
		return sporenet.Part{}, errors.New("campaign rigblock invalid")
	}
	part.Level = uint16(max(uint32(1), min(level, uint32(^uint16(0)))))
	part.Rarity = rarity
	err := c.rollBudgetedCampaignAffixes(&part, choice)
	if err != nil {
		return sporenet.Part{}, fmt.Errorf("partAffixes: %w", err)
	}
	err = c.ValidateGeneratedCampaignPart(part)
	if err != nil {
		return sporenet.Part{}, fmt.Errorf("partComplete: %w", err)
	}
	part.Cost = c.partCost(part.Level)
	return part, nil
}

// ValidateGeneratedCampaignPart rejects an incomplete generated descriptor
// before it can be published as a world pickup or granted to inventory.
func (e *PartCatalog) ValidateGeneratedCampaignPart(part sporenet.Part) error {
	if e == nil {
		return errors.New("campaign part catalog unavailable")
	}
	if part.Level == 0 || part.Rarity > sporenet.PartEpicUnique {
		return errors.New("campaign part level or rarity invalid")
	}
	_, isFound := e.ByRigblock(part.RigblockAssetID)
	if !isFound {
		return errors.New("campaign rigblock definition missing")
	}
	if part.SuffixAssetID == 0 {
		return errors.New("campaign suffix missing")
	}
	_, isFound = e.affixesByName[partAffixKey("suffix", part.SuffixAssetID)]
	if !isFound {
		return errors.New("campaign suffix definition missing")
	}
	rarity := campaignAffixRarity(part.Rarity)
	if rarity >= sporenet.PartRare && part.PrefixAssetID == 0 {
		return errors.New("campaign required prefix missing")
	}
	if rarity >= sporenet.PartEpic && part.PrefixSecondaryAssetID == 0 {
		return errors.New("campaign required second prefix missing")
	}
	for _, prefixID := range [...]uint16{part.PrefixAssetID, part.PrefixSecondaryAssetID} {
		if prefixID == 0 {
			continue
		}
		_, isFound = e.affixesByName[partAffixKey("prefix", prefixID)]
		if !isFound {
			return fmt.Errorf("campaign prefix definition %d missing", prefixID)
		}
	}
	if !e.isPartAffixValid(&part) {
		return errors.New("campaign affix modifiers invalid")
	}
	return nil
}

func (c *PartCatalog) campaignPartSlotType(
	classType string, scienceType string, creatureName string,
	campaignStage uint32, choice uint32,
	isUniqueFamily bool, itemLevels ...uint32,
) string {
	availableSlotTypes := c.campaignPartSlotTypes(
		classType, scienceType, creatureName, campaignStage, isUniqueFamily, itemLevels...,
	)
	if len(availableSlotTypes) == 0 {
		return ""
	}
	slotChoice := campaignPartChoice(choice, campaignSlotStream)
	return availableSlotTypes[slotChoice%uint32(len(availableSlotTypes))]
}

func (c *PartCatalog) campaignPartSlotTypes(
	classType string, scienceType string, creatureName string,
	campaignStage uint32, isUniqueFamily bool, itemLevels ...uint32,
) []string {
	availableSlotTypes := make([]string, 0, len(campaignPartSlotTypes))
	for _, slotType := range campaignPartSlotTypes {
		for _, definition := range c.partsByRigblock {
			if definition.MinimumLevel > definition.MaximumLevel || definition.SlotType != slotType ||
				definition.IsUniqueFamily != isUniqueFamily ||
				!c.isPartSlotUnlocked(definition, campaignStage) {
				continue
			}
			// Generation supplies the final rarity-adjusted item level. A
			// family existing at another level cannot make this slot viable.
			// Callers inspecting general slot availability may omit the level.
			if len(itemLevels) > 0 && !isCampaignRigblockEligible(
				definition, partSlotBit(slotType), campaignLootScienceMask,
				itemLevels[0], isUniqueFamily,
			) {
				continue
			}
			availableSlotTypes = append(availableSlotTypes, slotType)
			break
		}
	}
	return availableSlotTypes
}

func (e *CampaignPartSlotBag) selectSlotType(
	availableSlotTypes []string, choice uint32,
) (string, bool) {
	candidateSlotTypes := make([]string, 0, len(availableSlotTypes))
	for _, slotType := range availableSlotTypes {
		if _, isUsed := e.usedSlotTypes[slotType]; !isUsed {
			candidateSlotTypes = append(candidateSlotTypes, slotType)
		}
	}
	isRefill := len(candidateSlotTypes) == 0
	if isRefill {
		for _, slotType := range availableSlotTypes {
			if len(availableSlotTypes) > 1 && slotType == e.lastSlotType {
				continue
			}
			candidateSlotTypes = append(candidateSlotTypes, slotType)
		}
	}
	if len(candidateSlotTypes) == 0 {
		candidateSlotTypes = append(candidateSlotTypes, availableSlotTypes...)
	}
	return candidateSlotTypes[choice%uint32(len(candidateSlotTypes))], isRefill
}

func (e *CampaignPartSlotBag) recordSlotType(slotType string, isRefill bool) {
	if isRefill || e.usedSlotTypes == nil {
		e.usedSlotTypes = make(map[string]struct{}, len(campaignPartSlotTypes))
	}
	e.usedSlotTypes[slotType] = struct{}{}
	e.lastSlotType = slotType
}

func isCampaignPartSlotType(slotType string) bool {
	switch slotType {
	case "weapon", "grasper", "foot", "defense", "offense", "utility":
		return true
	default:
		return false
	}
}

func campaignPartLevelDistance(level uint32, definition PartDefinition) uint32 {
	if level < definition.MinimumLevel {
		return definition.MinimumLevel - level
	}
	if level > definition.MaximumLevel {
		return level - definition.MaximumLevel
	}
	return 0
}

// Slot availability uses mission stage; item-level and equipped-stat gates are separate.
func (e *PartCatalog) isPartSlotUnlocked(definition PartDefinition, campaignStage uint32) bool {
	if e == nil || e.tuning == nil {
		return true
	}
	minimumStage, isFound := e.tuning.SlotMinimumStages[definition.SlotType]
	return isFound && campaignStage >= minimumStage
}

func (c *PartCatalog) campaignPartRarity(difficulty uint32, choice uint32) sporenet.PartRarity {
	if c == nil || c.tuning == nil {
		return fallbackCampaignPartRarity(choice)
	}
	major := min((max(difficulty, uint32(1))+3)/4, uint32(18))
	uncommonChance := c.tuning.UncommonChances[major]
	rareChance := c.tuning.RareChances[major]
	epicChance := c.tuning.EpicChances[major]
	roll := float32(choice%100 + 1)
	switch {
	case roll > 100-epicChance:
		return sporenet.PartEpic
	case roll > 100-rareChance:
		return sporenet.PartRare
	case roll > 100-uncommonChance:
		return sporenet.PartUncommon
	default:
		return sporenet.PartBasic
	}
}

func (e *PartCatalog) campaignPartRarityFromBag(
	difficulty uint32, choice uint32, bag *CampaignPartRarityBag,
) sporenet.PartRarity {
	if bag == nil {
		return e.campaignPartRarity(difficulty, choice)
	}
	weights := e.campaignPartRarityWeights(difficulty)
	if bag.IsDestructorReward {
		// Destructor reward policy: 25% uncommon, 60% rare, 15% epic.
		weights = [4]float64{0, 25, 60, 15}
	}
	totalWeight := float64(0)
	for index := range weights {
		if !bag.IsDestructorReward && index > int(sporenet.PartBasic) {
			dryDrawCount := min(
				bag.dryDrawCounts[index], campaignRarityPityMaximumMisses,
			)
			weights[index] *= 1 +
				float64(dryDrawCount)*campaignRarityPityScales[index]
		}
		totalWeight += weights[index]
	}
	draw := float64(choice) / (float64(^uint32(0)) + 1) * totalWeight
	accumulatedWeight := float64(0)
	for index, weight := range weights {
		accumulatedWeight += weight
		if draw < accumulatedWeight {
			return sporenet.PartRarity(index)
		}
	}
	return sporenet.PartEpic
}

func (e *PartCatalog) campaignPartRarityWeights(difficulty uint32) [4]float64 {
	if e == nil || e.tuning == nil {
		return [4]float64{65, 27, 7, 1}
	}
	major := min((max(difficulty, uint32(1))+3)/4, uint32(18))
	uncommonChance := float64(e.tuning.UncommonChances[major])
	rareChance := float64(e.tuning.RareChances[major])
	epicChance := float64(e.tuning.EpicChances[major])
	return [4]float64{
		100 - uncommonChance,
		uncommonChance - rareChance,
		rareChance - epicChance,
		epicChance,
	}
}

func (e *CampaignPartRarityBag) recordRarity(rarity sporenet.PartRarity) {
	for index := int(sporenet.PartUncommon); index <= int(sporenet.PartEpic); index++ {
		if sporenet.PartRarity(index) == rarity {
			e.dryDrawCounts[index] = 0
			continue
		}
		e.dryDrawCounts[index] = min(
			e.dryDrawCounts[index]+1, campaignRarityPityMaximumMisses,
		)
	}
}

func fallbackCampaignPartRarity(choice uint32) sporenet.PartRarity {
	roll := choice % 100
	switch {
	case roll < 65:
		return sporenet.PartBasic
	case roll < 92:
		return sporenet.PartUncommon
	case roll < 99:
		return sporenet.PartRare
	default:
		return sporenet.PartEpic
	}
}

func campaignPartChoice(choice uint32, stream uint32) uint32 {
	choice += 0x9e3779b9 * (stream + 1)
	choice ^= choice >> 16
	choice *= 0x85ebca6b
	choice ^= choice >> 13
	choice *= 0xc2b2ae35
	choice ^= choice >> 16
	return choice
}

func isCampaignUniqueRarity(rarity sporenet.PartRarity) bool {
	return rarity >= sporenet.PartUnique && rarity <= sporenet.PartEpicUnique
}

func campaignAffixRarity(rarity sporenet.PartRarity) sporenet.PartRarity {
	if isCampaignUniqueRarity(rarity) {
		return rarity - sporenet.PartEpic
	}
	return rarity
}

func (e *PartCatalog) partCost(level uint16) uint32 {
	if e == nil || e.tuning == nil || e.tuning.PriceBase <= 0 ||
		e.tuning.PriceCurve <= 0 || e.tuning.PriceIncrement == 0 {
		return sporenet.FallbackPartCost(level)
	}
	if level == 0 {
		level = 1
	}
	// sub_9CD3D0 (103) and sub_9D0180 store pow to float32 before MULSS,
	// then truncate the float32 product to signed int32 with CVTTSS2SI.
	factor := float32(math.Pow(float64(e.tuning.PriceCurve), float64(level)))
	rawPrice := float32(e.tuning.PriceBase * factor)
	if math.IsNaN(float64(rawPrice)) || rawPrice < 0 {
		return sporenet.FallbackPartCost(level)
	}
	// Saturate rather than reproducing the native signed conversion overflow.
	if math.IsInf(float64(rawPrice), 0) || float64(rawPrice) >= float64(math.MaxInt32)+1 {
		return ^uint32(0)
	}
	// Widen after signed truncation so malformed large increments cannot wrap
	// the remainder or rounding addition into a negative price.
	price := int64(int32(rawPrice))
	priceIncrement := int64(e.tuning.PriceIncrement)
	remainder := price % priceIncrement
	price -= remainder
	threshold := int64(float32(e.tuning.PriceIncrement) * 0.5)
	if remainder >= threshold {
		if price > int64(^uint32(0))-priceIncrement {
			return ^uint32(0)
		}
		price += priceIncrement
	}
	return uint32(max(price, priceIncrement))
}

// PartCost returns the authored build-103 full price for one item level.
func (c *PartCatalog) PartCost(level uint16) uint32 {
	return c.partCost(level)
}

// IsFlairEligible reproduces build-103 sub_435820's rigblock content guard.
func (c *PartCatalog) IsFlairEligible(part *sporenet.Part) bool {
	if c == nil || part == nil {
		return false
	}
	definition, isFound := c.ByRigblock(part.RigblockAssetID)
	return isFound && definition.ContentFlags&0x70 == 0
}

// EquipmentScore derives the six-slot native item-point and gear-score values.
func (c *PartCatalog) EquipmentScore(parts []sporenet.Part) (float32, float32) {
	itemPoints := float32(300)
	if c == nil || c.tuning == nil {
		return 0, itemPoints
	}
	for index, part := range parts {
		if index >= 6 {
			break
		}
		itemPoints += 50 * (c.levelScale(uint32(part.Level)) - 1)
	}
	gearScale := itemPoints / 300
	gearScore := float32(math.Ceil(float64(c.inverseLevelScale(gearScale))))
	return gearScore, itemPoints
}

func (c *PartCatalog) inverseLevelScale(scale float32) float32 {
	if c == nil || c.tuning == nil || scale <= 1 {
		return 0
	}
	currentScale := float32(1)
	currentLevel := uint32(0)
	for _, band := range c.tuning.LevelBands {
		bandLength := band.MaximumLevel - currentLevel
		maximumScale := currentScale * partFloatPower(band.Base, bandLength)
		if scale <= maximumScale {
			fraction := math.Log(float64(scale/currentScale)) / math.Log(float64(band.Base))
			return float32(float64(currentLevel) + fraction)
		}
		currentScale = maximumScale
		currentLevel = band.MaximumLevel
	}
	return float32(currentLevel)
}

// Stats derives the native profile stat string for one persisted item.
func (c *PartCatalog) Stats(part *sporenet.Part) string {
	attributes, isFound := c.attributes(part)
	if !isFound {
		return " "
	}
	return formatPartStats(attributes)
}

func (c *PartCatalog) attributes(part *sporenet.Part) ([partAttributeCount]float32, bool) {
	if c == nil || c.tuning == nil || part == nil {
		return [partAttributeCount]float32{}, false
	}
	definition, isFound := c.ByRigblock(part.RigblockAssetID)
	if !isFound {
		return [partAttributeCount]float32{}, false
	}
	modifierBlocks, isAffixResolved := c.affixModifierBlocks(part)
	c.standardModifiers(&modifierBlocks[3], definition, uint32(part.Level), part.Rarity)
	modifierCount := positiveModifierCount(modifierBlocks)
	// sub_9CD3D0 rejects resolved affixes before scaling. Standard slot
	// modifiers cannot rescue an empty or nonpositive signed affix sum.
	if isAffixResolved && modifierCount == 0 {
		return [partAttributeCount]float32{}, false
	}
	levelScale := c.levelScale(uint32(part.Level))
	pointScale := levelScale * ((float32(modifierCount)*c.tuning.ExtraStatBonusFactor + 1) * c.tuning.BasePoint)
	rarity := int(part.Rarity)
	if rarity >= 4 && rarity <= 6 {
		rarity -= 3
	}
	if rarity < 0 || rarity >= len(c.tuning.RarityDistributions) {
		rarity = 0
	}
	distribution := c.tuning.RarityDistributions[rarity]
	distributions := [4]float32{distribution.Suffix, distribution.Prefix, distribution.Prefix2, distribution.Standard}
	var stats [partAttributeCount]float32
	specialInputs := [...]int{0, 1, 2, 4, 5, 10, 7, 9, 102, 103, 104, 105, 108}
	specialInputSet := make(map[int]bool, len(specialInputs))
	for _, input := range specialInputs {
		specialInputSet[input] = true
	}
	for blockIndex, modifiers := range modifierBlocks {
		for attributeIndex, modifier := range modifiers {
			if specialInputSet[attributeIndex] {
				continue
			}
			stats[attributeIndex] += modifier
		}
		for specialIndex, attributeIndex := range specialInputs {
			modifier := modifiers[attributeIndex]
			if modifier <= 0 {
				continue
			}
			amount := (distributions[blockIndex] * 0.01) * (modifier * 0.01) * pointScale
			pointCost := c.tuning.PointCosts[specialIndex]
			if pointCost > 0 {
				amount /= float32(pointCost)
			}
			stats[attributeIndex] += amount
		}
	}
	for _, attributeIndex := range specialInputs {
		stats[attributeIndex] = float32(int32(stats[attributeIndex]))
	}
	return stats, true
}

func (c *PartCatalog) runtimeAttributes(part *sporenet.Part) ([partAttributeCount]float32, bool) {
	attributes, isFound := c.attributes(part)
	if !isFound {
		return [partAttributeCount]float32{}, false
	}
	return normalizeRuntimePartAttributes(attributes), true
}

func normalizeRuntimePartAttributes(attributes [partAttributeCount]float32) [partAttributeCount]float32 {
	for attributeIndex := range attributes {
		if isPercentagePartAttribute(attributeIndex) {
			attributes[attributeIndex] *= 0.01
		}
	}
	return attributes
}

// isPercentagePartAttribute mirrors build-103 sub_9C99A0. The client keeps
// these values in whole-percent display units and sub_9C9A30 converts them to
// fractions only when a runtime caller requests normalized attributes.
func isPercentagePartAttribute(attributeIndex int) bool {
	switch attributeIndex {
	case 22, 23, 24, 26, 27, 35, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47,
		48, 50, 51, 52, 53, 62, 63, 64, 65, 66, 67, 68, 69, 70, 71, 72, 85, 86,
		87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 106, 107, 109, 111:
		return true
	default:
		return false
	}
}

// WeaponDamageModifier returns the native base-damage multiplier for a weapon item.
func (c *PartCatalog) WeaponDamageModifier(part *sporenet.Part) float32 {
	if c == nil || c.tuning == nil || part == nil {
		return 0
	}
	definition, isFound := c.ByRigblock(part.RigblockAssetID)
	if !isFound || definition.SlotType != "weapon" {
		return 0
	}
	if !c.isPartAffixValid(part) {
		return 0
	}
	return c.weaponDamageModifier(uint32(part.Level))
}

func (c *PartCatalog) weaponDamageModifier(level uint32) float32 {
	if c == nil || c.tuning == nil {
		return 0
	}
	if level < 1 {
		level = 1
	}
	return c.tuning.WeaponDamageMultiplier * c.levelScale(level)
}

// CriticalAttributes returns the native logical attributes used by the
// critical roll and multiplier for one item.
func (c *PartCatalog) CriticalAttributes(part *sporenet.Part) (CriticalAttributes, bool) {
	attributes, isFound := c.runtimeAttributes(part)
	if !isFound {
		return CriticalAttributes{}, false
	}
	return CriticalAttributes{
		Rating: attributes[10], AutoCrit: attributes[19], DamageIncrease: attributes[22],
	}, true
}

// affixModifierBlocks distinguishes resolved pointers from unresolved IDs,
// matching sub_9CD7C0 before the native stat-derivation validity guard.
func (e *PartCatalog) affixModifierBlocks(part *sporenet.Part) ([4][partAttributeCount]float32, bool) {
	var blocks [4][partAttributeCount]float32
	isAffixResolved := false
	for index, id := range [...]uint16{part.SuffixAssetID, part.PrefixAssetID, part.PrefixSecondaryAssetID} {
		kind := "prefix"
		if index == 0 {
			kind = "suffix"
		}
		affix, isFound := e.affixesByName[partAffixKey(kind, id)]
		if !isFound {
			continue
		}
		blocks[index] = affix.Modifiers
		isAffixResolved = true
	}
	return blocks, isAffixResolved
}

func (e *PartCatalog) isPartAffixValid(part *sporenet.Part) bool {
	blocks, isAffixResolved := e.affixModifierBlocks(part)
	return !isAffixResolved || positiveModifierCount(blocks) > 0
}

func (c *PartCatalog) copyAffixModifiers(target *[partAttributeCount]float32, kind string, id uint16) {
	if id == 0 {
		return
	}
	affix, isFound := c.affixesByName[partAffixKey(kind, id)]
	if !isFound {
		return
	}
	*target = affix.Modifiers
}

func (c *PartCatalog) standardModifiers(modifiers *[partAttributeCount]float32, definition PartDefinition, level uint32, rarity sporenet.PartRarity) {
	levelScale := float32(level)
	if levelScale < 1 {
		levelScale = 1
	}
	switch definition.SlotType {
	case "grasper":
		modifiers[26] = levelScale * c.tuning.HandLevelScale
		modifiers[9] = c.tuning.HandFlat
	case "foot":
		modifiers[48] = levelScale * c.tuning.FootLevelScale
		modifiers[7] = c.tuning.FootFlat
	case "offense":
		modifiers[10] = c.tuning.OffenseFlat
	case "defense":
		modifiers[4] = c.tuning.DefenseFlat
	case "utility":
		modifiers[5] = c.tuning.UtilityFlat
	case "weapon":
		if c.isArena {
			c.arenaWeaponModifiers(modifiers, definition, level, rarity)
			return
		}
		rarityOffset := int64(rarity) - 1
		if rarity >= 4 {
			rarityOffset = int64(rarity) - 4
		}
		rarityAdjustment := int64(c.tuning.RarityLevelStep) * rarityOffset
		// Signed integer division truncates toward zero, as in sub_9CB150.
		// Keep negative stages signed; only the stat magnitude clamps level.
		major := (int64(level) - rarityAdjustment) / int64(c.tuning.MajorLevelMultiplier)
		stage := 1 + int64(c.tuning.MinorStageCount)*(major-1)
		if definition.WeaponSlotType == "grasper" && stage >= int64(c.tuning.HandMinimumLevel) {
			modifiers[26] = levelScale * c.tuning.HandLevelScale
			return
		}
		if definition.WeaponSlotType == "foot" && stage >= int64(c.tuning.FootMinimumLevel) {
			modifiers[48] = levelScale * c.tuning.FootLevelScale
		}
	}
}

// forGameplayMode retains Arena's weapon-stat behavior without changing the
// shared catalog. Match-wide unranked item-level caps are applied to detached
// roster data before this catalog projects combat stats.
func (e *PartCatalog) forGameplayMode(mode Mode) *PartCatalog {
	if e == nil || mode != ModeArena {
		return e
	}
	catalog := *e
	catalog.isArena = true
	return &catalog
}

func (e *PartCatalog) arenaWeaponModifiers(modifiers *[partAttributeCount]float32, definition PartDefinition, level uint32, rarity sporenet.PartRarity) {
	if e.tuning.RarityLevelStep == 0 {
		return
	}
	rarityIndex := int64(rarity)
	if rarityIndex >= 4 && rarityIndex <= 6 {
		rarityIndex -= 3
	}
	normalizedLevel := max(int64(1), (int64(level)-int64(e.tuning.RarityLevelStep)*rarityIndex)/int64(e.tuning.RarityLevelStep))
	levelScale := float32(max(uint32(1), level))
	if definition.WeaponSlotType == "grasper" && normalizedLevel >= int64(e.tuning.HandMinimumLevel) {
		modifiers[26] = levelScale * e.tuning.HandLevelScale
		return
	}
	if definition.WeaponSlotType == "foot" && normalizedLevel >= int64(e.tuning.FootMinimumLevel) {
		modifiers[48] = levelScale * e.tuning.FootLevelScale
	}
}

func (c *PartCatalog) levelScale(level uint32) float32 {
	result := float32(1)
	currentLevel := uint32(0)
	for _, band := range c.tuning.LevelBands {
		if currentLevel >= level {
			break
		}
		exponent := level - currentLevel
		bandLength := band.MaximumLevel - currentLevel
		if exponent > bandLength {
			exponent = bandLength
		}
		result *= partFloatPower(band.Base, exponent)
		currentLevel += exponent
	}
	return result
}

func partFloatPower(base float32, exponent uint32) float32 {
	result := float32(1)
	for exponent > 0 {
		if exponent&1 != 0 {
			result *= base
		}
		exponent >>= 1
		if exponent > 0 {
			base *= base
		}
	}
	return result
}

func positiveModifierCount(blocks [4][partAttributeCount]float32) int {
	count := 0
	for attributeIndex := 0; attributeIndex < partAttributeCount; attributeIndex++ {
		// sub_9C9BE0 scans 115 attributes in build 103 (116 in 127).
		// Count each signed affix sum once, excluding standard slot block 3.
		// Preserve the two float32 additions: suffix + secondary + primary.
		total := float32(blocks[0][attributeIndex] + blocks[2][attributeIndex])
		total = float32(total + blocks[1][attributeIndex])
		if total > 0 {
			count++
		}
	}
	return count
}

func formatPartStats(stats [partAttributeCount]float32) string {
	var result strings.Builder
	for attributeIndex, amount := range stats {
		if amount == 0 {
			continue
		}
		token, isFound := partStatTokens[attributeIndex]
		if !isFound {
			continue
		}
		result.WriteString(token)
		result.WriteByte(',')
		result.WriteString(strconv.FormatFloat(float64(amount), 'f', 0, 32))
		result.WriteString(",0;")
	}
	if result.Len() == 0 {
		return " "
	}
	return result.String()
}

func partAffixKey(kind string, id uint16) string {
	return kind + "/" + strconv.FormatUint(uint64(id), 10)
}

var partStatTokens = map[int]string{
	0: "STR", 1: "DEX", 2: "MIND", 4: "HLTH", 5: "MANA", 7: "PDEF", 9: "EDEF", 10: "CRTR",
	22: "CRTD", 23: "ATTSP", 24: "COOL", 26: "PROS", 27: "AOERES", 35: "LFSTL", 37: "AOEDMG",
	38: "TCHDMG", 39: "QNTDMG", 40: "LFDMG", 41: "PLSDMG", 42: "NCRDMG", 43: "TCHRES",
	44: "QNTRES", 45: "LFRES", 46: "PLSRES", 47: "NCRRES", 48: "MOV", 50: "BUFD", 51: "DBUFD",
	52: "MNSTL", 53: "DBUFI", 60: "IBANISH", 61: "IKNCKB", 62: "AOERAD", 63: "PETD", 64: "PETH",
	65: "CRYS", 66: "DNA", 67: "RANGE", 68: "ORB", 69: "ODBLD", 70: "ODDUR", 71: "LOOT", 72: "SURE",
	73: "ISTUN", 75: "ISLEEP", 76: "ITAUNT", 77: "ITERROR", 78: "ISLNCE", 79: "ICURSE", 80: "IPOIS",
	81: "IBURN", 82: "IROOT", 83: "ISLOW", 84: "IPULL", 85: "DOTDMG", 86: "AGGROI", 87: "AGGROD",
	88: "PYDMG", 89: "PYADMG", 90: "EYDMG", 91: "EYADMG", 92: "CHAN", 93: "CCD", 94: "DOTDUR",
	95: "AOEDUR", 96: "HEAL", 101: "DPYINV", 102: "PYFLAT", 103: "EYFLAT", 104: "MINWD",
	105: "MAXWD", 106: "MINWDP", 107: "MAXWDP", 108: "ATTD", 109: "ATTDP", 111: "XP",
}
