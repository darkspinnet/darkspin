package loot

const (
	npcDropOrbBit       = uint32(2)
	npcDropCatalystBit  = uint32(4)
	npcDropEquipmentBit = uint32(8)
	npcDropDNABit       = uint32(16)
)

// NPCDropMask follows NonPlayerClass postload: an empty dropType array allows
// every category, while populated arrays combine their entries with bitwise OR.
func NPCDropMask(dropTypes []uint32) uint32 {
	if len(dropTypes) == 0 {
		return ^uint32(0)
	}
	mask := uint32(0)
	for _, dropType := range dropTypes {
		mask |= dropType
	}
	return mask
}

// IsEquipmentEmissionAllowed applies both native equipment gates before any
// attempt-count, chance, or descriptor-generation draw. Force is not a gate.
func IsEquipmentEmissionAllowed(dropMask uint32, isEquipmentDropEnabled bool) bool {
	return dropMask&npcDropEquipmentBit != 0 && isEquipmentDropEnabled
}

func IsNPCDropAllowed(dropTypes []uint32, kind NPCDropKind) bool {
	bit := uint32(0)
	switch kind {
	case NPCDropOrb:
		bit = npcDropOrbBit
	case NPCDropCrystal:
		bit = npcDropCatalystBit
	case NPCDropEquipment:
		bit = npcDropEquipmentBit
	case NPCDropDNA:
		bit = npcDropDNABit
	default:
		return false
	}
	return NPCDropMask(dropTypes)&bit != 0
}
