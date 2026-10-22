package gameplay

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	zoneinteract "github.com/darkspinnet/darkspin/server/zone/interact"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func isCampaignDestructorLoot(plan zonenpc.SpawnPlan) bool {
	return plan.OwnerObjectID == 0 && zoneboss.IsFinalBossNoun(plan.NounName)
}

type destructorEquipmentDrop struct {
	pickup      zoneinteract.EquipmentPickup
	destination sim.Position
	flight      sim.DropFlight
}

// Prepare the whole burst before publishing it; the caller owns the shared
// NPC reservation, so every participant sees the same world pickups.
func (e *gameplayPeerSession) spawnDestructorEquipment(
	enemy zonenpc.Snapshot, gameplayJoin *game.GameplayJoin, sourceTime uint64,
) ([][]byte, uint32, error) {
	if e == nil || gameplayJoin == nil || e.zone == nil || e.zone.DropRandom() == nil {
		return nil, 0, errors.New("destructor loot unavailable")
	}
	subjects := e.campaignPartSubjects()
	if len(subjects) == 0 {
		return nil, 0, errors.New("destructor loot roster unavailable")
	}
	source := e.destructorDropCenter(enemy)
	placement, err := zoneloot.NewPlacement(source, enemy.Plan.NPCProfile.FootprintRadius, e.zone.DropRandom())
	if err != nil {
		return nil, 0, fmt.Errorf("bossPlacement: %w", err)
	}
	participantCount := e.campaignLootParticipantCount()
	challenge, npcType := e.campaignNPCRewardSource(enemy.Plan, 500)
	if challenge <= 0 {
		return nil, 0, nil
	}
	attemptCount, err := zoneloot.EquipmentAttempts(npcType, participantCount, e.zone.DropRandom())
	if err != nil {
		return nil, 0, fmt.Errorf("bossAttempts: %w", err)
	}
	threshold, err := sim.EquipmentDropThreshold(int(participantCount), challenge, 0.45,
		1+e.campaignPartAttribute(campaignLootFindAttribute))
	if err != nil {
		return nil, 0, fmt.Errorf("bossThreshold: %w", err)
	}
	slotBag := game.CampaignPartSlotBag{}
	// Retain the existing server reward-quality policy; native boss flags
	// establish an item-level bonus, not these special rarity weights.
	rarityBag := game.CampaignPartRarityBag{IsDestructorReward: true}
	drops := make([]destructorEquipmentDrop, 0, attemptCount)
	packets := make([][]byte, 0)
	for index := uint32(0); index < attemptCount; index++ {
		if e.zone.DropRandom().Float64() >= float64(threshold) {
			continue
		}
		destination, placementErr := placement.Next(campaignDropProjector{mesh: e.zone.Navigation()})
		if placementErr != nil {
			return nil, 0, fmt.Errorf("bossDestination[%d]: %w", index, placementErr)
		}
		choice := e.zone.DropRandom().Uint32()
		subject := subjects[choice%uint32(len(subjects))]
		part, err := gameplayJoin.GenerateCampaignPartFromBag(
			subject, e.binding.Difficulty, e.binding.ChainLevelIndex, choice, &slotBag, &rarityBag, true,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("bossPart[%d]: %w", index, err)
		}
		err = gameplayJoin.ValidateGeneratedCampaignPart(part)
		if err != nil {
			return nil, 0, fmt.Errorf("bossPartComplete[%d]: %w", index, err)
		}
		objectID, err := e.reserveCampaignObjectID()
		if err != nil {
			return nil, 0, fmt.Errorf("bossObject[%d]: %w", index, err)
		}
		plan, err := zoneloot.PlanEquipment(zoneloot.EquipmentPlanInput{
			ObjectID: objectID, Rarity: zoneloot.Rarity(part.Rarity),
			PresentationPolicy: zoneloot.EquipmentUniqueRewardGroundDrop,
			Source:             source, Destination: destination,
			SimulationTime: time.Duration(sourceTime) * time.Millisecond,
		})
		if err != nil {
			return nil, 0, fmt.Errorf("bossPlan[%d]: %w", index, err)
		}
		dropPackets, err := e.marshalEquipmentDrop(plan, part)
		if err != nil {
			return nil, 0, fmt.Errorf("bossMarshal[%d]: %w", index, err)
		}
		packets = append(packets, dropPackets...)
		drops = append(drops, destructorEquipmentDrop{
			destination: destination,
			flight:      e.pickupFlight(plan.NounName, source, destination, plan.Lob),
			pickup: zoneinteract.EquipmentPickup{
				ObjectID: objectID, Part: part, WinnerRewardChoice: choice,
				PresentationPolicy:     zoneloot.EquipmentUniqueRewardGroundDrop,
				WinnerRewardDifficulty: e.binding.Difficulty,
				IsWinnerReward:         true, IsWinnerRewardBoss: true, IsDestructorReward: true,
			},
		})
	}
	for index, drop := range drops {
		err := e.registerCampaignPickup(zoneinteract.PickupEquipment, drop.pickup.ObjectID, source, drop.destination, drop.flight)
		if err != nil {
			e.removeDestructorDrops(drops[:index])
			return nil, 0, fmt.Errorf("bossRegister[%d]: %w", index, err)
		}
		err = e.zone.PickupPayload().AddEquipment(drop.pickup)
		if err != nil {
			e.removeDestructorDrops(drops[:index+1])
			return nil, 0, fmt.Errorf("bossTrack[%d]: %w", index, err)
		}
	}
	log.Printf("Campaign destructor equipment prepared actor=%d noun=%q participants=%d challenge=%d attempts=%d threshold=%g pickups=%d navigation=%t",
		enemy.Plan.ObjectID, enemy.Plan.NounName, participantCount, challenge, attemptCount,
		threshold, len(drops), e.zone.Navigation() != nil)
	if len(drops) == 0 {
		return packets, 0, nil
	}
	return packets, drops[0].pickup.ObjectID, nil
}

func (e *gameplayPeerSession) removeDestructorDrops(drops []destructorEquipmentDrop) {
	for _, drop := range drops {
		e.zone.Pickups().Remove(drop.pickup.ObjectID)
		e.zone.PickupPayload().RemoveEquipment(drop.pickup.ObjectID)
	}
}

type campaignDropProjector struct {
	mesh *navigation.Mesh
}

func (e *gameplayPeerSession) destructorDropCenter(enemy zonenpc.Snapshot) sim.Position {
	source := sim.Position(enemy.Plan.Position)
	nav := e.zone.Navigation()
	if nav == nil {
		return source
	}
	layer := uint8(0)
	if enemy.Navigation.IsPresent {
		layer = enemy.Navigation.PlanLayer
	}
	projection, err := nav.Project(navigation.Vec3(source), navigation.ProjectionOptions{
		PlanLayer: layer, MaxDistance: zonenavigation.ProjectionDistance,
	})
	if err != nil {
		// Native ignores this projection's status; retain the known source
		// rather than use an uninitialized center when projection is unavailable.
		return source
	}
	return sim.Position(projection.Position)
}

func (e campaignDropProjector) ProjectDropPosition(destination sim.Position) (sim.Position, bool) {
	if e.mesh == nil {
		return destination, true
	}
	layer, isLayerFound := e.mesh.SelectDropLayer()
	if !isLayerFound {
		return sim.Position{}, false
	}
	options := navigation.ProjectionOptions{PlanLayer: layer, MaxDistance: zonenavigation.ProjectionDistance}
	projection, err := e.mesh.Project(navigation.Vec3(destination), options)
	if err != nil {
		// Failed projection consumes this candidate; the sampler tries another.
		return sim.Position{}, false
	}
	return sim.Position(projection.Position), true
}
