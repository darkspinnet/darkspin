package gameplay

import (
	"errors"
	"fmt"
	"math"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

const (
	campaignMerakAddNounName      = "CryosPlasmaAdd.Noun"
	campaignMerakMaximumAddCount  = 20
	campaignMerakAddSpawnDistance = float32(2)
	campaignMerakDamageFraction   = float32(0.15)
)

type campaignMerakPassiveState struct {
	accruedDamage float32
	spawnCount    uint32
}

func (s *gameplayPeerSession) planCampaignMerakAdds(
	result zonenpc.DamageResult,
) ([]zonenpc.SpawnPlan, [][]byte, error) {
	if s == nil || s.zone == nil || s.zone.NPCs() == nil ||
		result.ObjectID == 0 || result.IsDamageImmune || result.Damage <= 0 {
		return nil, nil, nil
	}
	merak, isFound := s.zone.NPCs().NPC(result.ObjectID)
	if !isFound || merak.Plan.OwnerObjectID != 0 ||
		merak.Plan.NounName != "CryosBoss.Noun" {
		return nil, nil, nil
	}
	maximumHitPoint := merak.Plan.NPCProfile.HitPoint
	damageThreshold := maximumHitPoint * campaignMerakDamageFraction
	if damageThreshold <= 0 {
		return nil, nil, nil
	}
	if s.campaignMerakPassiveStates == nil {
		s.campaignMerakPassiveStates = make(map[uint32]campaignMerakPassiveState)
	}
	state := s.campaignMerakPassiveStates[result.ObjectID]
	state.accruedDamage += result.Damage
	requestedCount := int(state.accruedDamage / damageThreshold)
	if requestedCount == 0 {
		s.campaignMerakPassiveStates[result.ObjectID] = state
		return nil, nil, nil
	}
	state.accruedDamage -= float32(requestedCount) * damageThreshold
	availableCount := campaignMerakMaximumAddCount -
		s.zone.NPCs().OwnedActiveCount(result.ObjectID)
	if requestedCount > availableCount {
		requestedCount = availableCount
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
			IsRewardSuppressed: true, NPCProfile: npcProfile,
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
