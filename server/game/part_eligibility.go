package game

import (
	"strings"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// Native catalog collectors use slot/science intersections and item-level
// bounds. Class masks belong to equip rules and separate hero-targeted policy.
func isCampaignRigblockEligible(
	definition PartDefinition, slotMask uint32, scienceMask uint32,
	level uint32, isUnique bool,
) bool {
	return definition.IsUniqueFamily == isUnique &&
		partSlotBit(definition.SlotType)&slotMask != 0 &&
		partScienceMask(definition.ScienceType)&scienceMask != 0 &&
		definition.MinimumLevel <= level && level <= definition.MaximumLevel
}

func isCampaignAffixEligible(
	affix PartAffixDefinition, slotMask uint32, scienceMask uint32,
	level uint32, rarity sporenet.PartRarity,
) bool {
	if level < affix.MinimumLevel || level > affix.MaximumLevel ||
		partScienceMask(affix.ScienceType)&scienceMask == 0 {
		return false
	}
	affixSlotMask := uint32(0)
	for _, partType := range affix.PartTypes {
		affixSlotMask |= partSlotBit(partType)
	}
	if affixSlotMask&slotMask == 0 {
		return false
	}
	if affix.Kind == "suffix" {
		return affix.IsUniqueFamily == (rarity >= sporenet.PartUnique) &&
			(rarity != sporenet.PartBasic || affix.IsBasicEligible)
	}
	return affix.Kind == "prefix"
}

// Postload masks OR only recognized authored names; an empty mask stays zero.
func partSlotBit(slotType string) uint32 {
	switch strings.ToLower(strings.TrimSpace(slotType)) {
	case "defense":
		return 1 << 0
	case "offense":
		return 1 << 1
	case "utility":
		return 1 << 2
	case "flair":
		return 1 << 3
	case "grasper":
		return 1 << 4
	case "foot":
		return 1 << 5
	case "weapon":
		return 1 << 6
	default:
		return 0
	}
}

func partScienceMask(scienceList string) uint32 {
	mask := uint32(0)
	for scienceType := range strings.SplitSeq(scienceList, ",") {
		switch strings.ToLower(strings.TrimSpace(scienceType)) {
		case "cyber":
			mask |= 1 << 0
		case "chrono":
			mask |= 1 << 1
		case "bio":
			mask |= 1 << 2
		case "plasma":
			mask |= 1 << 3
		case "necro":
			mask |= 1 << 4
		}
	}
	return mask
}
