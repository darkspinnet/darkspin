package gameplay

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/inventory"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneinteract "github.com/darkspinnet/darkspin/server/zone/interact"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	lootsporenet "github.com/darkspinnet/darkspin/server/zone/loot/sporenet"
)

type gameplayInventoryRuntime struct {
	registry    *gameplaySessionRegistry
	progression inventory.PartDropper
	logger      *log.Logger
}

func (e gameplayInventoryRuntime) drop(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, error) {
	req, err := raknet.DecodeLootDropRequest(packet.Payload)
	if err != nil {
		return nil, fmt.Errorf("lootDropDecode: %w", err)
	}
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[packet.Address.String()]
	if !isFound || !peerSession.stage.IsDungeon() || peerSession.isZoneTerminal() ||
		peerSession.zone == nil || peerSession.zone.Pickups() == nil ||
		peerSession.zone.PickupPayload() == nil || peerSession.deployedObjectID == 0 {
		e.registry.mutex.Unlock()
		return nil, errors.New("lootDropSession: ground destination unavailable")
	}
	userID := peerSession.binding.UserID
	response, err := raknet.MarshalApplication(raknet.LootDiscardedMessage{
		ItemID: req.ItemID,
	})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("lootDropResponse: %w", err)
	}
	target := &gameplayPartDropTarget{session: &peerSession, sourceTime: packet.SourceTime}
	result := inventory.DropResult{}
	missionObjectID, isMissionItem := missionLootDropObjectID(req.ItemID)
	if isMissionItem {
		result.IsDropped, err = dropMissionEquipment(&peerSession, missionObjectID, target)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("lootDropMission: %w", err)
		}
	} else {
		result, err = inventory.Drop(ctx, e.progression, inventory.DropCommand{
			UserID: int64(userID), ItemID: req.ItemID,
		}, target)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("lootDropPart: %w", err)
		}
	}
	if result.IsDropped {
		e.registry.sessions[packet.Address.String()] = peerSession
		for sessionKey, candidate := range e.registry.sessions {
			if sessionKey == packet.Address.String() || candidate.zone != peerSession.zone ||
				!candidate.stage.IsDungeon() {
				continue
			}
			publishErr := candidate.publishPackets(target.packets)
			if publishErr != nil && e.logger != nil {
				e.logger.Printf("RakNet dropped item delivery queued user=%d object=%d: %v",
					candidate.binding.UserID, target.objectID, publishErr)
			}
			e.registry.sessions[sessionKey] = candidate
		}
	}
	e.registry.mutex.Unlock()
	if e.logger != nil {
		e.logger.Printf(
			"RakNet inventory drop user=%d item=%d mission_object=%d object=%d dropped=%t",
			userID, req.ItemID, missionObjectID, target.objectID, result.IsDropped,
		)
	}
	// Also acknowledge an already-absent item so retries repair stale client
	// inventory. Equipped-item rejection and save failure never reach this point.
	return append(target.packets, response), nil
}

func missionLootDropObjectID(itemID uint64) (uint32, bool) {
	const missionItemNamespace = uint64(1) << 32
	const missionItemLimit = missionItemNamespace << 1
	if itemID < missionItemNamespace || itemID >= missionItemLimit {
		return 0, false
	}
	objectID := uint32(itemID - missionItemNamespace)
	return objectID, objectID != 0
}

func dropMissionEquipment(
	peerSession *gameplayPeerSession, objectID uint32, target *gameplayPartDropTarget,
) (bool, error) {
	inventory := peerSession.zone.MissionEquipment(peerSession.binding.UserID)
	for _, equipment := range inventory.Equipments {
		if equipment.ObjectID != objectID {
			continue
		}
		part := lootsporenet.Part(equipment)
		err := target.PlacePart(part)
		if err != nil {
			target.RollbackPart()
			return false, fmt.Errorf("missionDropPlace: %w", err)
		}
		err = peerSession.zone.RemoveMissionEquipment(
			zone.Member{
				UserID: peerSession.binding.UserID, PeerGeneration: peerSession.generation,
			},
			objectID,
		)
		if err != nil {
			target.RollbackPart()
			return false, fmt.Errorf("missionDropRemove: %w", err)
		}
		return true, nil
	}
	return false, nil
}

// The registry lock keeps this provisional ground item uncollectable until
// its source inventory mutation succeeds. Packets are published afterward.
type gameplayPartDropTarget struct {
	session    *gameplayPeerSession
	sourceTime uint64
	objectID   uint32
	packets    [][]byte
}

func (e *gameplayPartDropTarget) PlacePart(part sporenet.Part) error {
	objectID, err := e.session.reserveCampaignObjectID()
	if err != nil {
		return fmt.Errorf("dropObject: %w", err)
	}
	e.objectID = objectID
	source := sim.Position(e.session.playerPosition)
	destination := e.session.reachableCampaignDropDestination(source)
	plan, err := zoneloot.PlanEquipment(zoneloot.EquipmentPlanInput{
		ObjectID: objectID, Rarity: zoneloot.Rarity(part.Rarity),
		PresentationPolicy: zoneloot.EquipmentInventoryGroundDrop,
		Source:             source, Destination: destination,
		SimulationTime: time.Duration(e.sourceTime) * time.Millisecond,
	})
	if err != nil {
		return fmt.Errorf("dropPlan: %w", err)
	}
	// Inventory identities are account-local and may be reused while the item
	// is on the ground. Collection allocates a new identity for the same affixes.
	part.ID = 0
	part.ReferenceID = 0
	e.packets, err = e.session.marshalEquipmentDrop(plan, part)
	if err != nil {
		return fmt.Errorf("dropMarshal: %w", err)
	}
	err = e.session.registerCampaignPickup(
		zoneinteract.PickupEquipment, objectID, source, destination,
		e.session.pickupFlight(plan.NounName, source, destination, plan.Lob),
	)
	if err != nil {
		return fmt.Errorf("dropRegister: %w", err)
	}
	err = e.session.zone.PickupPayload().AddEquipment(zoneinteract.EquipmentPickup{
		ObjectID: objectID, Part: part, WinnerUserID: e.session.binding.UserID,
		PresentationPolicy: zoneloot.EquipmentInventoryGroundDrop,
	})
	if err != nil {
		return fmt.Errorf("dropPayload: %w", err)
	}
	return nil
}

func (e *gameplayPartDropTarget) RollbackPart() {
	if e.objectID == 0 {
		return
	}
	e.session.zone.Pickups().Remove(e.objectID)
	e.session.zone.PickupPayload().RemoveEquipment(e.objectID)
	e.packets = nil
}
