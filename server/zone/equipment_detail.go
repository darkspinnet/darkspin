package zone

import (
	"context"
	"errors"
	"fmt"

	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
)

// ConvertMissionEquipmentToDetail validates the entire mission selection before
// the account save or any pending mission mutation. Mission equipment remains
// pending until the existing successful beam-out transaction commits it.
func (e *Zone) ConvertMissionEquipmentToDetail(
	ctx context.Context, member Member, req zoneloot.EquipmentDetailRequest,
	store zoneloot.OwnedPartDetailStore,
) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("detailContext: %w", err)
	}
	if len(req.ObjectIDs) == 0 || (len(req.OwnedPartIDs) != 0 && store == nil) {
		return errors.New("mission detail selection invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive || !e.isCurrentHumanMember(member) {
		return errors.New("mission detail member unavailable")
	}
	inventory := e.missionEquipments[member.UserID]
	if inventory.IsForfeited || inventory.IsCommitted {
		return errors.New("mission detail collection closed")
	}
	indexes := make([]int, 0, len(req.ObjectIDs))
	seenIDs := make(map[uint32]bool, len(req.ObjectIDs))
	for _, objectID := range req.ObjectIDs {
		if objectID == 0 || seenIDs[objectID] {
			return errors.New("mission detail identity invalid")
		}
		seenIDs[objectID] = true
		equipmentIndex := -1
		for index, equipment := range inventory.Equipments {
			if equipment.ObjectID == objectID {
				equipmentIndex = index
				break
			}
		}
		if equipmentIndex < 0 {
			return errors.New("mission detail equipment not found")
		}
		indexes = append(indexes, equipmentIndex)
	}
	if len(req.OwnedPartIDs) != 0 {
		err = store.ConvertOwnedPartsToDetail(ctx, req.OwnedPartIDs)
		if err != nil {
			return fmt.Errorf("detailOwned: %w", err)
		}
	}
	// No fallible operation follows the account save. A rejected account batch
	// leaves every mission item unchanged; successful repeat requests are inert.
	for _, index := range indexes {
		inventory.Equipments[index].IsDetail = true
	}
	e.missionEquipments[member.UserID] = inventory
	return nil
}
