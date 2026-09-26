package sporenet

import (
	"context"
	"fmt"

	storage "github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/zone/loot"
)

type EquipmentProgression interface {
	PartInventoryStatus(context.Context, int64) (storage.PartInventoryStatus, error)
	CommitMissionLoot(context.Context, int64, storage.MissionLoot) error
}

type EquipmentStore struct{ Progression EquipmentProgression }

func (e EquipmentStore) EquipmentCapacity(ctx context.Context, userID uint64) (uint32, uint32, error) {
	status, err := e.Progression.PartInventoryStatus(ctx, int64(userID))
	if err != nil {
		return 0, 0, fmt.Errorf("inventoryStatus: %w", err)
	}
	return status.OwnedCount, status.Capacity, nil
}

func (e EquipmentStore) CommitEquipment(ctx context.Context, req loot.EquipmentCompletion) error {
	parts := make([]storage.Part, 0, len(req.Inventory.Equipments))
	for _, equipment := range req.Inventory.Equipments {
		parts = append(parts, storage.Part{
			RigblockAssetID: equipment.RigblockID, PrefixAssetID: equipment.PrefixID,
			PrefixSecondaryAssetID: equipment.SecondaryPrefixID, SuffixAssetID: equipment.SuffixID,
			Level: equipment.Level, Rarity: storage.PartRarity(equipment.Rarity), Cost: equipment.Cost,
		})
	}
	err := e.Progression.CommitMissionLoot(ctx, int64(req.Inventory.UserID), storage.MissionLoot{
		CompletionID: req.CompletionID, Parts: parts,
		LimitedEditionPity: storage.LimitedEditionPity{
			MissCount: req.Inventory.LimitedEditionMissCount, UsedMask: req.Inventory.LimitedEditionUsedMask,
		},
		IsLimitedEditionPending: req.Inventory.IsLimitedEditionPending,
	})
	if err != nil {
		return fmt.Errorf("missionLoot: %w", err)
	}
	return nil
}

func MissionEquipment(objectID uint32, part storage.Part) loot.Equipment {
	return loot.Equipment{
		ObjectID: objectID, RigblockID: part.RigblockAssetID, PrefixID: part.PrefixAssetID,
		SecondaryPrefixID: part.PrefixSecondaryAssetID, SuffixID: part.SuffixAssetID,
		Level: part.Level, Rarity: uint8(part.Rarity), Cost: part.Cost,
	}
}
