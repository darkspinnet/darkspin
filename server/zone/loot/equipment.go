package loot

import "context"

// Equipment is mission-owned loot, not an account inventory item.
type Equipment struct {
	ObjectID          uint32
	RigblockID        uint16
	PrefixID          uint16
	SecondaryPrefixID uint16
	SuffixID          uint16
	Level             uint16
	Rarity            uint8
	Cost              uint32
}

type EquipmentInventory struct {
	UserID                  uint64
	Equipments              []Equipment
	LimitedEditionMissCount uint32
	LimitedEditionUsedMask  uint32
	IsLimitedEditionPending bool
	IsForfeited             bool
	IsCommitted             bool
}

func (e EquipmentInventory) Clone() EquipmentInventory {
	e.Equipments = append([]Equipment(nil), e.Equipments...)
	return e
}

type EquipmentCollection struct {
	Equipment               Equipment
	LimitedEditionMissCount uint32
	LimitedEditionUsedMask  uint32
	IsLimitedEditionPending bool
}

type EquipmentCompletion struct {
	CompletionID uint64
	Inventory    EquipmentInventory
}

// EquipmentStore persists a completed mission as one idempotent transaction.
type EquipmentStore interface {
	EquipmentCapacity(context.Context, uint64) (uint32, uint32, error)
	CommitEquipment(context.Context, EquipmentCompletion) error
}
