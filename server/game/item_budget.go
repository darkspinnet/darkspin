package game

import (
	"errors"
	"math"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// New-item policy only: select complete packaged affix combinations, never
// rewrite stored parts, combat attributes, or the client's stat calculation.
func (e *PartCatalog) rollBudgetedCampaignAffixes(
	part *sporenet.Part, choice uint32,
) error {
	if e == nil || e.tuning == nil || part == nil ||
		part.Rarity < sporenet.PartBasic || part.Rarity > sporenet.PartEpicUnique {
		return errors.New("campaign affix input invalid")
	}
	rigblock, isFound := e.ByRigblock(part.RigblockAssetID)
	if !isFound {
		return errors.New("campaign affix rigblock missing")
	}
	rarity := campaignAffixRarity(part.Rarity)
	slotMask := partSlotBit(rigblock.SlotType)
	scienceMask := partScienceMask(rigblock.ScienceType)
	suffixIDs := e.budgetAffixIDs(*part, "suffix", slotMask, scienceMask)
	prefixIDs := e.budgetAffixIDs(*part, "prefix", slotMask, scienceMask)
	slots := [][]uint16{suffixIDs}
	if rarity >= sporenet.PartRare {
		slots = append(slots, prefixIDs)
	}
	if rarity >= sporenet.PartEpic {
		slots = append(slots, prefixIDs)
	}
	if len(suffixIDs) == 0 {
		return errors.New("campaign suffix pool empty")
	}
	if rarity >= sporenet.PartRare && len(prefixIDs) == 0 {
		return errors.New("campaign required prefix pool empty")
	}
	baseBudget := generatedItemBudget(*part)
	// Packaged affixes are discrete. Use the smallest bounded ceiling that can
	// express a complete rarity-valid roll instead of making loot generation
	// (and therefore Cash Out) fail when the initial policy cap falls between
	// the available combinations.
	for _, multiplier := range [...]float32{1, 1.25, 1.5, 2, 3, 4} {
		candidate := *part
		random := newCampaignAffixRandom(choice)
		if e.fillBudgetedAffixes(
			&candidate, slots, 0, &random, baseBudget*multiplier,
		) {
			*part = candidate
			return nil
		}
	}
	return errors.New("campaign affix budget has no complete roll")
}

func (e *PartCatalog) budgetAffixIDs(
	part sporenet.Part, kind string, slotMask uint32, scienceMask uint32,
) []uint16 {
	ids := make([]uint16, 0, len(e.affixesByKind[kind]))
	for _, affix := range e.affixesByKind[kind] {
		if !isCampaignAffixEligible(affix, slotMask, scienceMask, uint32(part.Level), part.Rarity) {
			continue
		}
		ids = append(ids, affix.ID)
	}
	return ids
}

// Backtrack whole rolls rather than dropping an affix that exhausts the cap.
// Randomized starting positions allow an expensive affix to claim budget first;
// subsequent slots then find weaker companions. Catalog order is stable.
func (e *PartCatalog) fillBudgetedAffixes(
	part *sporenet.Part, slots [][]uint16, slot int, random *campaignAffixRandom,
	maximumBudget float32,
) bool {
	if slot == len(slots) {
		return true
	}
	ids := slots[slot]
	start := int(random.index(uint32(len(ids))))
	if slot == 2 {
		for len(ids) > 1 && ids[start] == part.PrefixAssetID {
			start = int(random.index(uint32(len(ids))))
		}
	}
	for offset := range ids {
		id := ids[(start+offset)%len(ids)]
		if slot == 2 && len(ids) > 1 && id == part.PrefixAssetID {
			continue
		}
		candidate := *part
		if slot == 0 {
			candidate.SetSuffix(id)
		} else {
			candidate.SetPrefix(id, slot == 2)
		}
		budget, isBudgetFound := e.generatedItemPower(candidate)
		if !isBudgetFound || budget > maximumBudget {
			continue
		}
		if e.fillBudgetedAffixes(
			&candidate, slots, slot+1, random, maximumBudget,
		) {
			*part = candidate
			return true
		}
	}
	return false
}

// campaignAffixRandom uses the recovered global LCG index calculation. Its
// per-item seed remains local policy until encounter RNG ownership is shared.
type campaignAffixRandom struct {
	state uint32
}

func newCampaignAffixRandom(seed uint32) campaignAffixRandom {
	if seed == 0 {
		seed = 0xAAAAAAAA
	}
	return campaignAffixRandom{state: seed}
}

func (e *campaignAffixRandom) index(count uint32) uint32 {
	product := uint64(1103515245)*uint64(e.state) + 12345
	e.state = uint32(product)
	return uint32(uint64(count) * uint64(uint32(product>>16)) >> 32)
}

func (e *PartCatalog) generatedItemPower(part sporenet.Part) (float32, bool) {
	scale := e.levelScale(uint32(part.Level))
	if scale <= 0 || math.IsNaN(float64(scale)) || math.IsInf(float64(scale), 0) {
		return 0, false
	}
	definition, isFound := e.ByRigblock(part.RigblockAssetID)
	if !isFound {
		return 0, false
	}
	var blocks [4][partAttributeCount]float32
	e.copyAffixModifiers(&blocks[0], "suffix", part.SuffixAssetID)
	e.copyAffixModifiers(&blocks[1], "prefix", part.PrefixAssetID)
	e.copyAffixModifiers(&blocks[2], "prefix", part.PrefixSecondaryAssetID)
	e.standardModifiers(&blocks[3], definition, uint32(part.Level), part.Rarity)
	// Budget policy clamps negatives before the shared affix-only count:
	// negatives cannot finance positives or suppress the stat-count premium.
	// Runtime attributes retain signed cancellation. Standard block 3 still
	// contributes to the budget's later stat calculation, not to this count.
	// Partial-roll pruning therefore remains monotonic and safe.
	for blockIndex := range blocks {
		for index, amount := range blocks[blockIndex] {
			if math.IsNaN(float64(amount)) || math.IsInf(float64(amount), 0) {
				return 0, false
			}
			blocks[blockIndex][index] = max(float32(0), amount)
		}
	}
	rarity := campaignAffixRarity(part.Rarity)
	distribution := e.tuning.RarityDistributions[rarity]
	shares := [4]float32{distribution.Suffix, distribution.Prefix, distribution.Prefix2, distribution.Standard}
	pointScale := scale *
		((float32(positiveModifierCount(blocks))*e.tuning.ExtraStatBonusFactor + 1) * e.tuning.BasePoint)
	var attributes [partAttributeCount]float32
	specials := [...]int{0, 1, 2, 4, 5, 10, 7, 9, 102, 103, 104, 105, 108}
	for blockIndex, modifiers := range blocks {
		for index, amount := range modifiers {
			attributes[index] += amount
		}
		for costIndex, index := range specials {
			amount := modifiers[index]
			attributes[index] -= amount
			cost := max(uint32(1), e.tuning.PointCosts[costIndex])
			attributes[index] += amount * 0.01 * shares[blockIndex] * 0.01 * pointScale / float32(cost)
		}
	}
	attributes = normalizeRuntimePartAttributes(attributes)
	budget := float32(0)
	for index, amount := range attributes {
		if amount <= 0 {
			continue
		}
		weight := generatedItemStatWeight(index, scale)
		if weight == 0 {
			// Build-103 vectors contain non-profile metadata channels. They are
			// neither displayed by Stats nor consumed by authoritative combat,
			// so they have no player power to price.
			continue
		}
		budget += amount * weight
	}
	return budget, true
}

func generatedItemBudget(part sporenet.Part) float32 {
	// Flat-stat weights already allow native level scaling. This additional
	// allowance grows slowly, especially for non-scaling percentage bonuses.
	cap := 65 + 0.6*min(float32(part.Level), 100) +
		15*float32(campaignAffixRarity(part.Rarity))
	if isCampaignUniqueRarity(part.Rarity) {
		cap += 10
	}
	return cap
}

// Weights are generation-policy points, not combat multipliers. Flat damage
// and speed are expensive on fast/low-base-damage heroes. Health is priced
// above critical rating because early armor can exceed a hero's base HP.
func generatedItemStatWeight(index int, scale float32) float32 {
	switch index {
	case 0, 1, 2:
		return 5 / scale
	case 4:
		return 1 / scale
	case 5:
		return 1.5 / scale
	case 7, 9:
		return 0.75 / scale
	case 10:
		return 0.5 / scale
	case 102, 103:
		return 8 / scale
	case 104, 105:
		return 12 / scale
	case 108:
		return 24 / scale
	case 23, 13, 28, 99, 106, 107, 109:
		return 200
	case 24, 70, 92:
		return 300
	case 35, 52:
		return 800
	case 60, 61, 73, 75, 76, 77, 78, 79, 80, 81, 82, 83, 84, 101:
		return 100
	default:
		if isPercentagePartAttribute(index) {
			return 150
		}
		return 0
	}
}
