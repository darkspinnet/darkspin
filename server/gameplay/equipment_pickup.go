package gameplay

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonecheckpoint "github.com/darkspinnet/darkspin/server/zone/checkpoint"
	zoneinteract "github.com/darkspinnet/darkspin/server/zone/interact"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	lootraknet "github.com/darkspinnet/darkspin/server/zone/loot/raknet103"
)

type campaignEquipmentPickupStep struct {
	runtime             campaignInteractionRuntime
	ctx                 context.Context
	sessionKey          string
	generation          uint64
	actorObjectID       uint32
	userID              uint64
	pickup              zoneinteract.EquipmentPickup
	deletePacket        []byte
	releasePacket       []byte
	rejectPacket        []byte
	progression         campaignLootProgression
	partBagCommit       campaignPartBagCommit
	winnerBagCommit     campaignEquipmentWinnerBag
	isWinnerBagPending  bool
	fullInventoryUserID uint64
	fullInventoryStatus sporenet.PartInventoryStatus
}

// prepareLocked runs only at the collection boundary, after target validation.
// Capacity is checked before consuming winner RNG or generating a winner item.
func (e *campaignEquipmentPickupStep) prepareLocked(currentSession gameplayPeerSession) error {
	inventoryReader, isInventoryReader := e.progression.(campaignInventoryReader)
	if !isInventoryReader {
		return errors.New("equipment capacity unavailable")
	}
	if e.pickup.WinnerUserID == 0 {
		e.fullInventoryUserID = currentSession.binding.UserID
		participants := e.runtime.equipmentRollParticipantsLocked(currentSession)
		eligibleParticipants := make([]zoneloot.EquipmentRollParticipant, 0, len(participants))
		for _, participant := range participants {
			inventoryStatus, err := inventoryReader.PartInventoryStatus(e.ctx, int64(participant.UserID))
			if err != nil {
				return fmt.Errorf("equipmentCapacity[%d]: %w", participant.UserID, err)
			}
			inventoryStatus = campaignMissionInventoryStatus(currentSession.zone, participant.UserID, inventoryStatus)
			if participant.UserID == currentSession.binding.UserID {
				e.fullInventoryUserID = participant.UserID
				e.fullInventoryStatus = inventoryStatus
			}
			if inventoryStatus.IsFull {
				continue
			}
			eligibleParticipants = append(eligibleParticipants, participant)
		}
		if len(eligibleParticipants) == 0 {
			return fmt.Errorf("equipmentEligible: %w", zone.ErrMissionInventoryFull)
		}
		rollResult, pendingWinnerBag, err := currentSession.campaignEquipmentWinnerBag.roll(
			eligibleParticipants, currentSession.zone.DropRandom(),
		)
		if err != nil {
			return fmt.Errorf("equipmentRoll: %w", err)
		}
		pickupRolls := make([]zoneinteract.EquipmentPickupRoll, 0, len(rollResult.Rolls))
		for _, roll := range rollResult.Rolls {
			pickupRolls = append(pickupRolls, zoneinteract.EquipmentPickupRoll{
				UserID: roll.UserID, ObjectID: roll.ObjectID, Roll: roll.Roll,
			})
		}
		selectedPickup, err := currentSession.zone.PickupPayload().SetEquipmentRoll(
			e.pickup.ObjectID, rollResult.Winner.UserID, pickupRolls,
		)
		if err != nil {
			return fmt.Errorf("equipmentWinner: %w", err)
		}
		e.pickup = selectedPickup
		e.winnerBagCommit = pendingWinnerBag
		e.isWinnerBagPending = true
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("RakNet campaign equipment roll target=%d winner_user=%d rolls=%v",
				e.pickup.ObjectID, e.pickup.WinnerUserID, rollResult.Rolls)
		}
	}
	e.userID = e.pickup.WinnerUserID
	inventoryStatus, err := inventoryReader.PartInventoryStatus(e.ctx, int64(e.userID))
	if err != nil {
		return fmt.Errorf("winnerCapacity: %w", err)
	}
	inventoryStatus = campaignMissionInventoryStatus(currentSession.zone, e.userID, inventoryStatus)
	if inventoryStatus.IsFull {
		e.fullInventoryUserID = e.userID
		e.fullInventoryStatus = inventoryStatus
		clearErr := currentSession.zone.PickupPayload().ClearEquipmentRoll(e.pickup.ObjectID, e.userID)
		if clearErr != nil {
			return fmt.Errorf("winnerReset: %w", errors.Join(zone.ErrMissionInventoryFull, clearErr))
		}
		return fmt.Errorf("winnerFull: %w", zone.ErrMissionInventoryFull)
	}
	if !e.pickup.IsWinnerReward {
		return nil
	}
	winnerSession := gameplayPeerSession{}
	isWinnerFound := false
	for _, candidate := range e.runtime.registry.sessions {
		if candidate.zone != currentSession.zone || candidate.binding.UserID != e.userID ||
			candidate.isZoneTerminal() {
			continue
		}
		winnerSession = candidate
		isWinnerFound = true
		break
	}
	if !isWinnerFound {
		clearErr := currentSession.zone.PickupPayload().ClearEquipmentRoll(e.pickup.ObjectID, e.userID)
		if clearErr != nil {
			return fmt.Errorf("winnerReset: %w", clearErr)
		}
		return errors.New("equipment winner session unavailable")
	}
	winnerPart, pendingCommit, err := winnerSession.materializeCampaignWinnerPart(
		e.ctx, e.runtime.gameplayJoin, e.progression, e.pickup,
	)
	if err == nil {
		var materializedPickup zoneinteract.EquipmentPickup
		materializedPickup, err = currentSession.zone.PickupPayload().SetEquipmentWinnerPart(
			e.pickup.ObjectID, e.userID, winnerPart,
		)
		if err == nil {
			e.pickup = materializedPickup
		}
	}
	if err != nil {
		clearErr := currentSession.zone.PickupPayload().ClearEquipmentRoll(e.pickup.ObjectID, e.userID)
		if clearErr != nil {
			err = errors.Join(err, fmt.Errorf("winnerReset: %w", clearErr))
		}
		return fmt.Errorf("winnerPart: %w", err)
	}
	e.partBagCommit = pendingCommit
	return nil
}

func (e *campaignEquipmentPickupStep) notifyInventoryFull(gameID uint32) {
	err := e.runtime.gameplayJoin.PublishInventoryFull(e.ctx, int64(e.fullInventoryUserID), gameID,
		e.fullInventoryStatus.OwnedCount, e.fullInventoryStatus.Capacity)
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("RakNet campaign equipment full-inventory notice failed user=%d object=%d: %v",
			e.fullInventoryUserID, e.pickup.ObjectID, err)
	}
}

// finish completes the animation at its absolute 400 ms deadline. Collection
// already happened at 100 ms; this step never rolls, grants, or deletes loot.
func (e *campaignEquipmentPickupStep) finish() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || peerSession.generation != e.generation || peerSession.isZoneTerminal() {
		return nil, nil
	}
	isScheduled := peerSession.campaignScheduleSession().Remove(
		zoneaction.ScheduleEquipment, e.pickup.ObjectID, nil,
	)
	if !isScheduled {
		return nil, nil
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	return [][]byte{e.releasePacket}, nil
}

func (e *campaignEquipmentPickupStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation && !peerSession.isZoneTerminal()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	sourceSession := peerSession
	if !peerSession.campaignScheduleSession().Has(zoneaction.ScheduleEquipment, e.pickup.ObjectID) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	pickup, isPickupFound := sourceSession.zone.PickupPayload().Equipment(e.pickup.ObjectID)
	if !isPickupFound || !sourceSession.zone.Pickups().IsReserved(e.pickup.ObjectID) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if sourceSession.deployedObjectID != e.actorObjectID || sourceSession.deployedHitPoint() <= 0 {
		sourceSession.zone.Pickups().Release(e.pickup.ObjectID)
		e.runtime.registry.mutex.Unlock()
		return [][]byte{e.rejectPacket}, nil
	}
	e.pickup = pickup
	prepareErr := e.prepareLocked(sourceSession)
	if prepareErr != nil {
		sourceSession.zone.Pickups().Release(e.pickup.ObjectID)
		e.runtime.registry.mutex.Unlock()
		if errors.Is(prepareErr, zone.ErrMissionInventoryFull) {
			e.notifyInventoryFull(sourceSession.binding.GameID)
			return [][]byte{e.rejectPacket}, nil
		}
		return nil, fmt.Errorf("equipmentPrepare: %w", prepareErr)
	}
	winnerMember := zone.Member{}
	for _, candidate := range e.runtime.registry.sessions {
		if candidate.zone == sourceSession.zone && candidate.binding.UserID == e.userID &&
			!candidate.isZoneTerminal() {
			winnerMember = zoneResultMember(candidate)
			break
		}
	}
	grantedPart, err := e.collectMissionEquipment(sourceSession.zone, winnerMember)
	if err != nil {
		if errors.Is(err, zone.ErrMissionInventoryFull) {
			clearErr := sourceSession.zone.PickupPayload().ClearEquipmentRoll(
				e.pickup.ObjectID, e.userID,
			)
			if clearErr != nil && e.runtime.logger != nil {
				e.runtime.logger.Printf(
					"RakNet campaign equipment delayed full winner reset failed user=%d object=%d: %v",
					e.userID, e.pickup.ObjectID, clearErr,
				)
			}
		}
		sourceSession.zone.Pickups().Release(e.pickup.ObjectID)
		e.runtime.registry.mutex.Unlock()
		if errors.Is(err, zone.ErrMissionInventoryFull) {
			e.runtime.logger.Printf(
				"RakNet campaign equipment retained for full inventory user=%d object=%d",
				e.userID, e.pickup.ObjectID,
			)
			if isCurrent {
				return [][]byte{e.rejectPacket}, nil
			}
			return nil, nil
		}
		return nil, fmt.Errorf("campaignEquipmentGrant: %w", err)
	}
	if e.partBagCommit.isPending {
		for candidateSessionKey, candidate := range e.runtime.registry.sessions {
			if candidate.zone != sourceSession.zone || candidate.binding.UserID != e.userID {
				continue
			}
			candidate.campaignPartSlotBag = e.partBagCommit.partSlotBag
			candidate.campaignWeaponSubjectBag = e.partBagCommit.weaponSubjectBag
			candidate.campaignPartRarityBag = e.partBagCommit.partRarityBag
			e.runtime.registry.sessions[candidateSessionKey] = candidate
		}
	}
	if e.isWinnerBagPending {
		for candidateSessionKey, candidate := range e.runtime.registry.sessions {
			if candidate.zone != sourceSession.zone {
				continue
			}
			candidate.campaignEquipmentWinnerBag = e.winnerBagCommit.Clone()
			e.runtime.registry.sessions[candidateSessionKey] = candidate
		}
	}
	winnerSessionKey := ""
	winnerSession := gameplayPeerSession{}
	for candidateSessionKey, candidate := range e.runtime.registry.sessions {
		if candidate.zone != sourceSession.zone ||
			candidate.binding.UserID != e.userID {
			continue
		}
		winnerSessionKey = candidateSessionKey
		winnerSession = candidate
		break
	}
	awardPacket := []byte(nil)
	if winnerSessionKey != "" {
		awardPacket, err = lootraknet.MarshalEquipmentAward(
			grantedPart, uint8(winnerSession.binding.Slot), winnerSession.deployedObjectID,
			winnerSession.playerPosition,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEquipmentAward: %w", err)
		}
	}
	rollPackets := make([][]byte, 0, len(e.pickup.Rolls))
	if len(e.pickup.Rolls) > 1 {
		for index, roll := range e.pickup.Rolls {
			rollPacket, rollErr := raknet.MarshalApplication(raknet.LootRollMessage{
				ObjectID: roll.ObjectID, Roll: roll.Roll,
			})
			if rollErr != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf(
					"campaignEquipmentRollMarshal[%d]: %w", index, rollErr,
				)
			}
			rollPackets = append(rollPackets, rollPacket)
		}
	}
	responsePackets := make([][]byte, 0, 3)
	for candidateSessionKey, candidate := range e.runtime.registry.sessions {
		if candidate.zone != sourceSession.zone {
			continue
		}
		packets := append([][]byte(nil), rollPackets...)
		if candidateSessionKey == winnerSessionKey {
			packets = append(packets, awardPacket)
		}
		packets = append(packets, e.deletePacket)
		if candidateSessionKey == e.sessionKey && isCurrent {
			responsePackets = append(responsePackets, packets...)
			continue
		}
		publishErr := candidate.publishPackets(packets)
		if publishErr != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf(
				"RakNet campaign equipment pickup peer delivery queued game=%d user=%d object=%d: %v",
				candidate.binding.GameID, candidate.binding.UserID,
				e.pickup.ObjectID, publishErr,
			)
		}
		e.runtime.registry.sessions[candidateSessionKey] = candidate
	}
	e.runtime.registry.mutex.Unlock()
	sourceSession.zone.SaveCheckpointIfSafe(zonecheckpoint.ReasonSafePickup)
	if winnerSessionKey == "" && e.runtime.logger != nil {
		e.runtime.logger.Printf(
			"RakNet campaign equipment granted without connected winner presentation user=%d object=%d",
			e.userID, e.pickup.ObjectID,
		)
	}
	return responsePackets, nil
}
