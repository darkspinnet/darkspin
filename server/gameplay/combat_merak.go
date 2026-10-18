package gameplay

import (
	"errors"
	"fmt"
	"math"
	"strings"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

const (
	campaignMerakAddNounName      = "CryosPlasmaAdd.Noun"
	campaignMerakMaximumAddCount  = 20
	campaignMerakAddSpawnDistance = float32(2)
)

type campaignMerakPassiveState struct {
	accruedDamage float32
	spawnCount    uint32
}

func (s *gameplayPeerSession) planCampaignMerakAdds(
	result zonenpc.DamageResult,
) ([]zonenpc.SpawnPlan, [][]byte, error) {
	if s == nil || s.zone == nil || s.zone.NPCs() == nil ||
		result.ObjectID == 0 || result.IsDamageImmune || result.Damage <= 0 ||
		math.IsNaN(float64(result.Damage)) || math.IsInf(float64(result.Damage), 0) {
		return nil, nil, nil
	}
	merak, isFound := s.zone.NPCs().NPC(result.ObjectID)
	if !isFound || merak.Plan.OwnerObjectID != 0 {
		return nil, nil, nil
	}
	switch strings.ToLower(merak.Plan.NounName) {
	case "cryosboss.noun", "cryosboss_2.noun", "cryosboss_3.noun":
	default:
		return nil, nil, nil
	}
	rank := merak.Plan.NPCProfile.NPCRank
	damageFractions := [...]float32{0.15, 0.10, 0.05}
	if rank < 1 || rank > int32(len(damageFractions)) {
		return nil, nil, nil
	}
	// The live profile contains the effective, run-scaled maximum health.
	maximumHitPoint := merak.Plan.NPCProfile.HitPoint
	damageThreshold := maximumHitPoint * damageFractions[rank-1]
	if damageThreshold <= 0 || math.IsNaN(float64(damageThreshold)) ||
		math.IsInf(float64(damageThreshold), 0) {
		return nil, nil, nil
	}
	if s.campaignMerakPassiveStates == nil {
		s.campaignMerakPassiveStates = make(map[uint32]campaignMerakPassiveState)
	}
	state := s.campaignMerakPassiveStates[result.ObjectID]
	state.accruedDamage += result.Damage
	if math.IsNaN(float64(state.accruedDamage)) ||
		math.IsInf(float64(state.accruedDamage), 0) {
		return nil, nil, nil
	}
	// Chunk 607 retains the full accumulator at equality.
	if state.accruedDamage <= damageThreshold {
		s.campaignMerakPassiveStates[result.ObjectID] = state
		return nil, nil, nil
	}
	earnedBudget := state.accruedDamage / damageThreshold
	if math.IsInf(float64(earnedBudget), 0) {
		return nil, nil, nil
	}
	earnedCount := float32(math.Floor(float64(earnedBudget)))
	// Spend the entire earned budget before applying the existing owner cap.
	state.accruedDamage -= earnedCount * damageThreshold
	if math.IsNaN(float64(state.accruedDamage)) ||
		math.IsInf(float64(state.accruedDamage), 0) {
		return nil, nil, nil
	}
	availableCount := campaignMerakMaximumAddCount -
		s.zone.NPCs().OwnedActiveCount(result.ObjectID)
	requestedCount := max(0, availableCount)
	if earnedCount < float32(requestedCount) {
		requestedCount = int(earnedCount)
	}
	if requestedCount <= 0 {
		s.campaignMerakPassiveStates[result.ObjectID] = state
		return nil, nil, nil
	}

	director := s.zone.DirectorDefinition()
	npcProfile, isProfileFound := director.NPCProfilesByNoun["cryosplasmaadd.noun"]
	actionProfile, isActionFound := zonenpc.ActionProfileForNoun(
		campaignMerakAddNounName,
	)
	if !isProfileFound || !npcProfile.IsKnown || !isActionFound {
		return nil, nil, errors.New("Merak add profile unavailable")
	}
	targetObjectID := merak.TargetObjectID
	if targetObjectID == 0 {
		targetObjectID = s.deployedObjectID
	}
	if targetObjectID == 0 {
		return nil, nil, errors.New("Merak add target unavailable")
	}
	plans := make([]zonenpc.SpawnPlan, 0, requestedCount)
	packets := make([][]byte, 0, requestedCount*4)
	for addIndex := 0; addIndex < requestedCount; addIndex++ {
		objectID, err := s.reserveCampaignObjectID()
		if err != nil {
			return nil, nil, fmt.Errorf("merakAddReserve[%d]: %w", addIndex, err)
		}
		angle := float64(state.spawnCount%8) * math.Pi / 4
		position := merak.Plan.Position
		position.X += float32(math.Cos(angle)) * campaignMerakAddSpawnDistance
		position.Y += float32(math.Sin(angle)) * campaignMerakAddSpawnDistance
		state.spawnCount++
		plan := zonenpc.SpawnPlan{
			ObjectID: objectID, OwnerObjectID: result.ObjectID,
			NounName: campaignMerakAddNounName, Position: position,
			LocusID: merak.Plan.LocusID, MarkerSetName: merak.Plan.MarkerSetName,
			IsEncounterAuxiliary: true, NPCProfile: npcProfile,
			ActionProfile: actionProfile, IsActionKnown: true,
		}
		spawnPackets, err := npcraknet.TargetedSpawn(plan, targetObjectID)
		if err != nil {
			return nil, nil, fmt.Errorf("merakAddMarshal[%d]: %w", addIndex, err)
		}
		plans = append(plans, plan)
		packets = append(packets, spawnPackets...)
	}
	err := s.zone.NPCs().Add(plans, targetObjectID)
	if err != nil {
		return nil, nil, fmt.Errorf("merakAdd: %w", err)
	}
	err = s.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{
		Plans: plans, TargetObjectID: targetObjectID,
	}, s.binding.UserID, s.generation)
	if err != nil {
		rollbackErr := s.zone.NPCs().RollbackAdd(plans)
		return nil, nil, fmt.Errorf(
			"merakAddPublish: %w", errors.Join(err, rollbackErr),
		)
	}
	s.campaignMerakPassiveStates[result.ObjectID] = state
	return plans, packets, nil
}
