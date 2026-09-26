package zone

import (
	"context"
	"errors"
	"fmt"

	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
)

var ErrMissionInventoryFull = errors.New("mission inventory full")

func (e *Zone) MissionEquipment(userID uint64) zoneloot.EquipmentInventory {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.missionEquipments[userID].Clone()
}

func (e *Zone) CollectMissionEquipment(
	ctx context.Context, member Member, store zoneloot.EquipmentStore,
	req zoneloot.EquipmentCollection,
) error {
	if store == nil || req.Equipment.ObjectID == 0 || req.Equipment.RigblockID == 0 {
		return errors.New("mission equipment request invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive || !e.isCurrentMember(member) {
		return errors.New("mission equipment member unavailable")
	}
	inventory := e.missionEquipments[member.UserID]
	if inventory.IsForfeited || inventory.IsCommitted {
		return errors.New("mission equipment collection closed")
	}
	for _, equipment := range inventory.Equipments {
		if equipment.ObjectID == req.Equipment.ObjectID {
			return errors.New("mission equipment already collected")
		}
	}
	ownedCount, capacity, err := store.EquipmentCapacity(ctx, member.UserID)
	if err != nil {
		return fmt.Errorf("equipmentCapacity: %w", err)
	}
	if uint64(ownedCount)+uint64(len(inventory.Equipments)) >= uint64(capacity) {
		return ErrMissionInventoryFull
	}
	pickup, isPickupFound := e.info.PickupPayload.Equipment(req.Equipment.ObjectID)
	if !isPickupFound || pickup.WinnerUserID != member.UserID ||
		!e.info.Pickups.Commit(req.Equipment.ObjectID) {
		return errors.New("mission equipment pickup unavailable")
	}
	inventory.UserID = member.UserID
	inventory.Equipments = append(inventory.Equipments, req.Equipment)
	if req.IsLimitedEditionPending {
		inventory.LimitedEditionMissCount = req.LimitedEditionMissCount
		inventory.LimitedEditionUsedMask = req.LimitedEditionUsedMask
		inventory.IsLimitedEditionPending = true
	}
	e.missionEquipments[member.UserID] = inventory
	e.info.PickupPayload.RemoveEquipment(req.Equipment.ObjectID)
	return nil
}

// Only the ordinary successful beam-out reservation can settle mission loot.
// Abort, disconnect, and squad defeat never call the account grant operation.
func (e *Zone) CommitMissionEquipment(
	ctx context.Context, member Member, store zoneloot.EquipmentStore,
) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive || !e.isCurrentMember(member) || store == nil ||
		e.info.Boss.Snapshot().Phase != zoneboss.PhaseComplete {
		return errors.New("mission equipment completion unavailable")
	}
	bossObjectID, isReserved := e.info.Outcome.ReservedBossObjectID(outcomeMember(member))
	if !isReserved || bossObjectID == 0 {
		return errors.New("mission equipment result not reserved")
	}
	inventory := e.missionEquipments[member.UserID]
	if inventory.IsCommitted || inventory.IsForfeited || len(inventory.Equipments) == 0 {
		return nil
	}
	err := store.CommitEquipment(ctx, zoneloot.EquipmentCompletion{
		CompletionID: e.completionID, Inventory: inventory.Clone(),
	})
	if err != nil {
		return fmt.Errorf("equipmentCommit: %w", err)
	}
	inventory.IsCommitted = true
	e.missionEquipments[member.UserID] = inventory
	return nil
}

func (e *Zone) forfeitMissionEquipment(userID uint64) {
	inventory := e.missionEquipments[userID]
	if inventory.IsCommitted || inventory.IsForfeited {
		return
	}
	e.missionEquipments[userID] = zoneloot.EquipmentInventory{
		UserID: userID, IsForfeited: true,
	}
	if e.info.Checkpoint != nil {
		e.info.Checkpoint.ForfeitEquipment(e.id, userID)
	}
}
