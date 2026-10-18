package gameplay

import (
	"errors"
	"fmt"
	"log"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zoneunlock "github.com/darkspinnet/darkspin/server/zone/unlock"
)

// Use retained zone membership, including temporarily disconnected members.
// Native uses the stored participant count, not the living/status-filtered
// recipient count. Rejoin must not add a second attempt for the same member.
func (e *gameplayPeerSession) campaignLootParticipantCount() uint32 {
	if e == nil || e.zone == nil {
		return 1
	}
	return max(uint32(1), uint32(len(e.zone.Snapshot().Members)))
}

func campaignNPCLootSource(plan zonenpc.SpawnPlan, fallbackChallenge int32) (int32, uint32) {
	if plan.NPCProfile.IsClassKnown {
		return plan.NPCProfile.ChallengeValue, plan.NPCProfile.NPCType
	}
	// Unresolved classes retain explicit compatibility values. Do not infer
	// the native active Elite.NPCAffix bonus merely from a captain/elite label.
	if plan.IsBoss {
		return fallbackChallenge, 2
	}
	return fallbackChallenge, 0
}

func (e *gameplayPeerSession) removeUnpublishedLoot(objectIDs []uint32) {
	for _, objectID := range objectIDs {
		isPickupRemoved := e.zone.Pickups().Remove(objectID)
		isEquipmentRemoved := e.zone.PickupPayload().RemoveEquipment(objectID)
		isCrystalRemoved := e.zone.PickupPayload().RemoveCrystal(objectID)
		// Only one payload kind exists for an ID; absence of the other is
		// expected. A missing pickup or both payloads indicates stale cleanup.
		if !isPickupRemoved || (!isEquipmentRemoved && !isCrystalRemoved) {
			log.Printf("Campaign unpublished loot cleanup incomplete object=%d pickup=%t equipment=%t crystal=%t", objectID, isPickupRemoved, isEquipmentRemoved, isCrystalRemoved)
		}
	}
}

func (e *gameplayPeerSession) spawnCampaignCrystalAttempts(
	req game.CampaignScriptInvocation, definitions []sim.CrystalDefinition,
	offsets []sim.CrystalLevelOffset, sourceTime uint64, npcType uint32,
	selectionBag *campaignCrystalSelectionBag,
) ([][]byte, []uint32, error) {
	if e == nil || e.zone == nil {
		return nil, nil, errors.New("crystal emitter unavailable")
	}
	if !zoneunlock.AreCatalystDropsUnlocked(e.binding) || len(definitions) == 0 || req.Challenge <= 0 {
		return nil, nil, nil
	}
	attemptCount, err := zoneloot.CrystalAttempts(npcType, e.campaignLootParticipantCount(), e.zone.DropRandom())
	if err != nil {
		return nil, nil, fmt.Errorf("crystalAttempts: %w", err)
	}
	packets := make([][]byte, 0)
	objectIDs := make([]uint32, 0, attemptCount)
	for attempt := uint32(0); attempt < attemptCount; attempt++ {
		// Complete successful definition selection before the next chance draw.
		dropPackets, objectID, dropErr := e.spawnCampaignCrystal(req, definitions, offsets, sourceTime, nil, selectionBag)
		if objectID != 0 {
			objectIDs = append(objectIDs, objectID)
		}
		if dropErr != nil {
			e.removeUnpublishedLoot(objectIDs)
			return nil, nil, fmt.Errorf("crystalAttempt[%d]: %w", attempt, dropErr)
		}
		packets = append(packets, dropPackets...)
	}
	return packets, objectIDs, nil
}

func (s *gameplayPeerSession) spawnCampaignNPCEquipment(
	enemy zonenpc.Snapshot,
	gameplayJoin *game.GameplayJoin,
	sourceTime uint64,
	isLootBagEnabled bool,
) ([][]byte, uint32, error) {
	if s == nil || s.zone == nil || !enemy.IsDefeated || enemy.Plan.ObjectID == 0 {
		return nil, 0, errors.New("campaign enemy equipment unavailable")
	}
	dropMask := zoneloot.NPCDropMask(enemy.Plan.NPCProfile.DropTypes)
	if !zoneloot.IsEquipmentEmissionAllowed(
		dropMask, s.zone.DirectorDefinition().IsEquipmentDropEnabled,
	) {
		if isCampaignDestructorLoot(enemy.Plan) {
			log.Printf("Campaign destructor equipment blocked actor=%d noun=%q mask=%#x equipment_enabled=%t",
				enemy.Plan.ObjectID, enemy.Plan.NounName, dropMask,
				s.zone.DirectorDefinition().IsEquipmentDropEnabled)
		}
		return nil, 0, nil
	}
	challenge, npcType := campaignNPCLootSource(enemy.Plan, campaignNPCEquipmentSourceAmount)
	if challenge <= 0 && !isCampaignDestructorLoot(enemy.Plan) {
		return nil, 0, nil
	}
	reservation, isReserved := s.reserveCampaignNPCDrop(
		enemy.Plan.ObjectID, zoneloot.NPCDropEquipment,
	)
	if !isReserved {
		return nil, 0, nil
	}
	if isCampaignDestructorLoot(enemy.Plan) {
		packets, objectID, err := s.spawnDestructorEquipment(enemy, gameplayJoin, sourceTime)
		if err != nil {
			reservation.Release()
			return nil, 0, fmt.Errorf("destructorEquipment: %w", err)
		}
		err = reservation.Commit()
		if err != nil {
			// Preserve the completed special reward's publication on failure;
			// retain its reservation so a retry cannot generate another batch.
			return packets, objectID, fmt.Errorf("destructorCommit: %w", err)
		}
		return packets, objectID, nil
	}
	attemptCount, err := zoneloot.EquipmentAttempts(npcType, s.campaignLootParticipantCount(), s.zone.DropRandom())
	if err != nil {
		reservation.Release()
		return nil, 0, fmt.Errorf("equipmentAttempts: %w", err)
	}
	// Optional compatibility pity is separate from the native occurrence rule.
	// Ordinary death emission passes false; recipient item bags remain separate.
	pendingEquipmentDropBag := campaignEquipmentDropBag{}
	equipmentDropBag := (*campaignEquipmentDropBag)(nil)
	if isLootBagEnabled {
		pendingEquipmentDropBag = s.campaignEquipmentDropBag
		equipmentDropBag = &pendingEquipmentDropBag
	}
	packets := make([][]byte, 0)
	objectIDs := make([]uint32, 0, attemptCount)
	for attempt := uint32(0); attempt < attemptCount; attempt++ {
		dropPackets, objectID, roll, dropErr := s.spawnCampaignEquipmentWithPolicy(
			game.CampaignScriptInvocation{Position: enemy.Plan.Position, Challenge: challenge},
			gameplayJoin, sourceTime, dropMask, npcType == 2, false, "", nil,
			equipmentDropBag, nil, true,
		)
		if objectID != 0 {
			objectIDs = append(objectIDs, objectID)
		}
		if dropErr != nil {
			s.removeUnpublishedLoot(objectIDs)
			reservation.Release()
			return nil, 0, fmt.Errorf("equipmentAttempt[%d]: %w", attempt, dropErr)
		}
		if objectID != 0 && roll.Part.RigblockAssetID == 0 {
			s.removeUnpublishedLoot(objectIDs)
			reservation.Release()
			return nil, 0, errors.New("campaign equipment roll incomplete")
		}
		packets = append(packets, dropPackets...)
		if equipmentDropBag != nil {
			equipmentDropBag.recordDrop(objectID != 0)
		}
	}
	err = reservation.Commit()
	if err != nil {
		s.removeUnpublishedLoot(objectIDs)
		reservation.Release()
		return nil, 0, fmt.Errorf("enemyEquipmentCommit: %w", err)
	}
	if isLootBagEnabled {
		s.campaignEquipmentDropBag = pendingEquipmentDropBag
	}
	if len(objectIDs) == 0 {
		return packets, 0, nil
	}
	return packets, objectIDs[0], nil
}

func (s *gameplayPeerSession) spawnCampaignNPCCrystal(
	enemy zonenpc.Snapshot,
	definitions []sim.CrystalDefinition,
	offsets []sim.CrystalLevelOffset,
	sourceTime uint64,
) ([][]byte, uint32, error) {
	if s == nil || s.zone == nil || !enemy.IsDefeated || enemy.Plan.ObjectID == 0 {
		return nil, 0, errors.New("campaign enemy crystal unavailable")
	}
	if !zoneloot.IsNPCDropAllowed(enemy.Plan.NPCProfile.DropTypes, zoneloot.NPCDropCrystal) {
		return nil, 0, nil
	}
	if enemy.Plan.IsFixture && !zonenpc.IsVerdanthTotem(enemy.Plan) {
		return nil, 0, nil
	}
	fallbackChallenge := campaignNPCCrystalSourceAmount
	if zonenpc.IsVerdanthTotem(enemy.Plan) {
		fallbackChallenge = campaignVerdanthTotemCrystalSourceAmount
	}
	challenge, npcType := campaignNPCLootSource(enemy.Plan, fallbackChallenge)
	if challenge <= 0 || !zoneunlock.AreCatalystDropsUnlocked(s.binding) || len(definitions) == 0 {
		return nil, 0, nil
	}
	reservation, isReserved := s.reserveCampaignNPCDrop(
		enemy.Plan.ObjectID, zoneloot.NPCDropCrystal,
	)
	if !isReserved {
		return nil, 0, nil
	}
	pendingSelectionBag := s.campaignCrystalSelectionBag.Clone()
	packets, objectIDs, err := s.spawnCampaignCrystalAttempts(
		game.CampaignScriptInvocation{Position: enemy.Plan.Position, Challenge: challenge},
		definitions, offsets, sourceTime, npcType, &pendingSelectionBag,
	)
	if err != nil {
		reservation.Release()
		return nil, 0, fmt.Errorf("enemyCrystalSpawn: %w", err)
	}
	err = reservation.Commit()
	if err != nil {
		s.removeUnpublishedLoot(objectIDs)
		reservation.Release()
		return nil, 0, fmt.Errorf("enemyCrystalCommit: %w", err)
	}
	if len(objectIDs) == 0 {
		return packets, 0, nil
	}
	return packets, objectIDs[0], nil
}
