package hero

import (
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/util"
)

const blitzAppearanceAsset = uint64(0xa93aff21)
const sageAppearanceAsset = uint64(0x55d1408f)

var wraithNouns = map[uint32]struct{}{
	0xD6189D41: {}, // PC_SN_Tank.Noun
	0xFA748ACB: {}, // PC_SN_Tank_v1.Noun
	0x2CEE8592: {}, // PC_SN_Tank_v2.Noun
	0xF8FE7CDD: {}, // PC_SN_Tank_v3.Noun
}

func IsBasicAbility(
	definitionByNoun map[uint32]sim.AbilityDefinition, noun uint32, name string,
) bool {
	if noun == 0 || name == "" {
		return false
	}
	definition, isFound := definitionByNoun[noun]
	return isFound && definition.Name == name
}

func IsWraith(noun uint32) bool {
	_, isFound := wraithNouns[noun]
	return isFound
}

func IsWraithActiveRequest(noun uint32, abilityIndex uint32) bool {
	return IsWraith(noun) && abilityIndex == 2
}

func IsWraithRandomRequest(noun uint32, abilityIndex uint32) bool {
	return IsWraith(noun) && abilityIndex == 3
}

func CreatureCount(binding game.GameplayBinding) uint32 {
	var count uint32
	for _, creature := range binding.Creatures {
		if creature.Noun != 0 {
			count++
		}
	}
	if count == 0 {
		return 1
	}
	return count
}

func AppearanceAsset(creature game.GameplayCreature) uint64 {
	if creature.ID != 0 && creature.AppearanceVersion > 1 {
		// Build 103's native appearance cache is keyed by asset ID alone,
		// not by revision. Saved revisions are reserved across accounts, so
		// include that revision to distinguish account-local creature IDs.
		return uint64(creature.AppearanceVersion)<<32 | uint64(creature.ID)
	}
	if creature.ID != 0 && creature.Noun != 0 {
		// Revision 1 loads the noun's shipped template. Different nouns must
		// not share an account-local ID in the native appearance cache either.
		return uint64(creature.Noun)
	}
	if creature.Noun == util.HashID("PC_LF_Mage.Noun") {
		return sageAppearanceAsset
	}
	// Synthetic bindings have no persistent creature identity. Preserve the
	// recovered tutorial appearance as their compatibility fallback.
	return blitzAppearanceAsset
}

// ObjectID allocates one stable squad block per admitted player slot.
func ObjectID(playerSlot uint16, creatureIndex uint32) uint32 {
	return 1 + uint32(playerSlot)*squad.Size + creatureIndex
}

// FirstSharedObjectID reserves every possible player squad before zone-owned
// fixtures, NPCs, effects, and loot begin allocating identifiers.
func FirstSharedObjectID() uint32 {
	return ObjectID(game.MaxGamePlayers, 0)
}
