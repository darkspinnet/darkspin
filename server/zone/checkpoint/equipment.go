package checkpoint

import (
	"context"
	"fmt"

	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
)

// ForfeitEquipment invalidates a defeated squad's loot in an older safe
// checkpoint without advancing that checkpoint into an unsafe encounter.
func (e *Manager) ForfeitEquipment(zoneID uint64, userID uint64) {
	if e == nil || e.store == nil || zoneID == 0 || userID == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.forfeitedEquipments[zoneID] == nil {
		e.forfeitedEquipments[zoneID] = make(map[uint64]struct{})
	}
	e.forfeitedEquipments[zoneID][userID] = struct{}{}
	e.pendingForfeits[zoneID] = struct{}{}
	if snapshot, isPending := e.pendingSnapshots[zoneID]; isPending {
		e.pendingSnapshots[zoneID] = checkpointWithoutEquipment(snapshot, e.forfeitedEquipments[zoneID])
	}
	if !e.isClosing {
		select {
		case e.wake <- struct{}{}:
		default:
		}
	}
}

func checkpointWithoutEquipment(snapshot Snapshot, userIDs map[uint64]struct{}) Snapshot {
	if len(userIDs) == 0 {
		return snapshot
	}
	snapshot = cloneSnapshot(snapshot)
	for userID := range userIDs {
		isMember := false
		for _, member := range snapshot.Members {
			if member.UserID == userID {
				isMember = true
				break
			}
		}
		if !isMember {
			continue
		}
		isFound := false
		for index, inventory := range snapshot.MissionEquipments {
			if inventory.UserID != userID {
				continue
			}
			if !inventory.IsCommitted {
				snapshot.MissionEquipments[index] = zoneloot.EquipmentInventory{UserID: userID, IsForfeited: true}
			}
			isFound = true
			break
		}
		if !isFound {
			snapshot.MissionEquipments = append(snapshot.MissionEquipments,
				zoneloot.EquipmentInventory{UserID: userID, IsForfeited: true})
		}
	}
	return snapshot
}

func (e *Manager) flushEquipmentForfeits() bool {
	for {
		e.mu.Lock()
		zoneID := uint64(0)
		for pendingZoneID := range e.pendingForfeits {
			zoneID = pendingZoneID
			break
		}
		e.mu.Unlock()
		if zoneID == 0 {
			return true
		}
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		err := e.saveEquipmentForfeit(ctx, zoneID)
		cancel()
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("Zone loot forfeiture failed zone=%d: %v", zoneID, err)
			}
			return false
		}
	}
}

func (e *Manager) saveEquipmentForfeit(ctx context.Context, zoneID uint64) error {
	e.storeMu.Lock()
	defer e.storeMu.Unlock()
	snapshot, isFound, err := e.store.Load(ctx, zoneID)
	if err != nil {
		return fmt.Errorf("forfeitLoad: %w", err)
	}
	// Serialize with new forfeitures so a concurrent squad wipe stays queued.
	e.mu.Lock()
	defer e.mu.Unlock()
	if isFound {
		snapshot = checkpointWithoutEquipment(snapshot, e.forfeitedEquipments[zoneID])
		err = e.store.Save(ctx, snapshot)
		if err != nil {
			return fmt.Errorf("forfeitSave: %w", err)
		}
	}
	delete(e.pendingForfeits, zoneID)
	return nil
}
