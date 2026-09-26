package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	lootsporenet "github.com/darkspinnet/darkspin/server/zone/loot/sporenet"
)

func campaignMissionInventoryStatus(
	current *zone.Zone, userID uint64, status sporenet.PartInventoryStatus,
) sporenet.PartInventoryStatus {
	inventory := current.MissionEquipment(userID)
	status.OwnedCount = uint32(min(uint64(^uint32(0)),
		uint64(status.OwnedCount)+uint64(len(inventory.Equipments))))
	status.IsFull = status.Capacity == 0 || status.OwnedCount >= status.Capacity ||
		inventory.IsForfeited || inventory.IsCommitted
	return status
}

func (e campaignEquipmentPickupStep) collectMissionEquipment(
	current *zone.Zone, member zone.Member,
) (sporenet.Part, error) {
	progression, isSupported := e.progression.(lootsporenet.EquipmentProgression)
	if !isSupported {
		return sporenet.Part{}, errors.New("mission equipment store unavailable")
	}
	err := current.CollectMissionEquipment(e.ctx, member,
		lootsporenet.EquipmentStore{Progression: progression}, zoneloot.EquipmentCollection{
			Equipment:               lootsporenet.MissionEquipment(e.pickup.ObjectID, e.pickup.Part),
			LimitedEditionMissCount: e.partBagCommit.limitedEditionPity.MissCount,
			LimitedEditionUsedMask:  e.partBagCommit.limitedEditionPity.UsedMask,
			IsLimitedEditionPending: e.partBagCommit.isLimitedEditionPending,
		})
	if err != nil {
		return sporenet.Part{}, fmt.Errorf("missionCollect: %w", err)
	}
	part := e.pickup.Part
	// LootAwarded needs nonzero display identities. Keep provisional IDs outside
	// the persistent inventory ID range; only successful completion allocates it.
	part.ID = uint64(1)<<32 | uint64(e.pickup.ObjectID)
	part.ReferenceID = uint64(1)<<63 | uint64(e.pickup.ObjectID)
	part.CreationDate = uint64(time.Now().Unix())
	return part, nil
}
