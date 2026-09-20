package zone

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zonepopulation "github.com/darkspinnet/darkspin/server/zone/population"
)

type PopulationRequest struct {
	Position game.Vec3
}

type PopulationTransition struct {
	Decisions  []zonepopulation.Decision
	SpawnPlans []zonenpc.SpawnPlan
	Acquired   []zonenpc.Snapshot
}

func (e *Zone) PrimePopulation(
	req PopulationRequest,
) (PopulationTransition, error) {
	if e == nil {
		return PopulationTransition{}, errors.New("nil zone")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return PopulationTransition{}, errors.New("zone inactive")
	}
	transition := PopulationTransition{}
	if !e.isPopulationPrimed {
		initialNPCPlans, err := e.introduceInitialNPCs(req.Position)
		if err != nil {
			return PopulationTransition{}, fmt.Errorf("initialNPCIntroduce: %w", err)
		}
		transition.SpawnPlans = append(transition.SpawnPlans, initialNPCPlans...)
		if len(e.info.FixturePlans) != 0 {
			err = e.info.NPCs.AddDormant(e.info.FixturePlans)
			if err != nil {
				return PopulationTransition{}, fmt.Errorf("fixtureAdd: %w", err)
			}
		}
		transition.Decisions, err = e.info.Population.PrimeOpening(req.Position)
		if err != nil {
			return PopulationTransition{}, fmt.Errorf("openingPrime: %w", err)
		}
		introducedPlans, err := e.planPopulation(transition.Decisions)
		if err != nil {
			return PopulationTransition{}, fmt.Errorf("openingPlan: %w", err)
		}
		transition.SpawnPlans = append(transition.SpawnPlans, introducedPlans...)
		e.isPopulationPrimed = true
	}
	targets := e.livePlayerAlignedTargets()
	published, err := e.info.NPCs.PublishInTargetRange(targets)
	if err != nil {
		return PopulationTransition{}, fmt.Errorf("openingPublish: %w", err)
	}
	for _, npc := range published {
		transition.SpawnPlans = append(transition.SpawnPlans, npc.Plan)
	}
	acquired, err := e.info.NPCs.AcquireTargets(targets, true)
	if err != nil {
		return PopulationTransition{}, fmt.Errorf("openingAcquire: %w", err)
	}
	transition.Acquired = acquired
	return transition, nil
}

func (e *Zone) AdvancePopulation(
	req PopulationRequest,
) (PopulationTransition, error) {
	if e == nil {
		return PopulationTransition{}, errors.New("nil zone")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return PopulationTransition{}, errors.New("zone inactive")
	}
	initialNPCPlans, err := e.introduceInitialNPCs(req.Position)
	if err != nil {
		return PopulationTransition{}, fmt.Errorf("initialNPCIntroduce: %w", err)
	}
	decisions, err := e.info.Population.Observe(req.Position, true)
	if err != nil {
		return PopulationTransition{}, fmt.Errorf("populationObserve: %w", err)
	}
	introducedPlans, err := e.planPopulation(decisions)
	if err != nil {
		return PopulationTransition{}, fmt.Errorf("populationPlan: %w", err)
	}
	targets := e.livePlayerAlignedTargets()
	published, err := e.info.NPCs.PublishInTargetRange(targets)
	if err != nil {
		return PopulationTransition{}, fmt.Errorf("populationPublish: %w", err)
	}
	spawnPlans := make([]zonenpc.SpawnPlan, 0, len(introducedPlans)+len(published))
	spawnPlans = append(spawnPlans, introducedPlans...)
	for _, npc := range published {
		spawnPlans = append(spawnPlans, npc.Plan)
	}
	acquired, err := e.info.NPCs.AcquireTargets(targets, true)
	if err != nil {
		return PopulationTransition{}, fmt.Errorf("populationAcquire: %w", err)
	}
	return PopulationTransition{
		Decisions:  decisions,
		SpawnPlans: append(initialNPCPlans, spawnPlans...),
		Acquired:   acquired,
	}, nil
}

// introduceInitialNPCs publishes authored fixed actors only when they can also
// acquire the approaching player. An untargeted publication lets the retail
// client move the actor autonomously before server authority starts pursuit.
func (e *Zone) introduceInitialNPCs(position game.Vec3) ([]zonenpc.SpawnPlan, error) {
	if len(e.info.InitialNPCPlans) == 0 {
		return nil, nil
	}
	introducedObjectIDs := make(map[uint32]bool)
	for _, npc := range e.info.NPCs.Snapshots() {
		introducedObjectIDs[npc.Plan.ObjectID] = true
	}
	introductionRadius := e.info.NPCs.AggroRadius()
	if introductionRadius <= 0 {
		return nil, errors.New("initial npc aggro radius unavailable")
	}
	radiusSquared := introductionRadius * introductionRadius
	plans := make([]zonenpc.SpawnPlan, 0)
	for _, plan := range e.info.InitialNPCPlans {
		if introducedObjectIDs[plan.ObjectID] {
			continue
		}
		deltaX := position.X - plan.Position.X
		deltaY := position.Y - plan.Position.Y
		deltaZ := position.Z - plan.Position.Z
		distanceSquared := deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ
		if distanceSquared > radiusSquared {
			continue
		}
		plans = append(plans, plan)
	}
	if len(plans) == 0 {
		return nil, nil
	}
	err := e.info.NPCs.AddDormant(plans)
	if err != nil {
		return nil, fmt.Errorf("initialNPCAdd: %w", err)
	}
	return plans, nil
}

func (e *Zone) planPopulation(
	decisions []zonepopulation.Decision,
) ([]zonenpc.SpawnPlan, error) {
	occupiedSpawnGroups := make(map[uint32]struct{})
	operativeCount := 0
	for spawnGroupID := range e.clearedSpawnGroups {
		occupiedSpawnGroups[spawnGroupID] = struct{}{}
	}
	for _, npc := range e.info.NPCs.Snapshots() {
		// Defeated actors remain in durable snapshots and spend the map budget.
		profile, isOperative := zonenpc.OperativeProfile(npc.Plan.NounName)
		if isOperative && zonenpc.IsOperativeCage(profile.ModifierName) {
			operativeCount++
		}
		if npc.Plan.LocusID != 0 {
			occupiedSpawnGroups[npc.Plan.LocusID] = struct{}{}
		}
	}
	filteredDecisions := make([]zonepopulation.Decision, 0, len(decisions))
	for _, decision := range decisions {
		if decision.LocusID != 0 {
			if _, isOccupied := occupiedSpawnGroups[decision.LocusID]; isOccupied {
				continue
			}
		}
		filteredDecisions = append(filteredDecisions, decision)
	}
	plans, _, err := e.info.Population.PlanCampaignSpawns(
		e.info.DirectorDefinition, filteredDecisions, e.info.ObjectID.Next(),
		e.info.ChainLevelIndex,
	)
	if err != nil {
		return nil, fmt.Errorf("spawnPlan: %w", err)
	}
	if len(plans) == 0 {
		return nil, nil
	}
	connectedCount := 0
	for _, member := range e.members {
		if member.IsConnected {
			connectedCount++
		}
	}
	plans = e.info.Population.AddOperatives(
		e.info.DirectorDefinition, plans, connectedCount > 1, operativeCount,
	)
	firstObjectID, err := e.info.ObjectID.Reserve(uint32(len(plans)))
	if err != nil {
		return nil, fmt.Errorf("objectIDReserve: %w", err)
	}
	for index := range plans {
		plans[index].ObjectID = firstObjectID + uint32(index)
	}
	err = e.info.NPCs.CanAddDormant(plans)
	if err != nil {
		return nil, fmt.Errorf("enemyValidate: %w", err)
	}
	introducedPlans := make([]zonenpc.SpawnPlan, 0, len(plans))
	stagedPlans := make([]zonenpc.SpawnPlan, 0, len(plans))
	for _, plan := range plans {
		switch plan.Introduction {
		case zonenpc.SpawnIntroductionFloorWarp,
			zonenpc.SpawnIntroductionDormant,
			zonenpc.SpawnIntroductionAmbush:
			introducedPlans = append(introducedPlans, plan)
		default:
			stagedPlans = append(stagedPlans, plan)
		}
	}
	if len(introducedPlans) != 0 {
		err = e.info.NPCs.AddDormant(introducedPlans)
		if err != nil {
			return nil, fmt.Errorf("enemyIntroduce: %w", err)
		}
	}
	if len(stagedPlans) != 0 {
		err = e.info.NPCs.AddStaged(stagedPlans)
		if err != nil {
			return nil, fmt.Errorf("enemyStage: %w", err)
		}
	}
	return introducedPlans, nil
}
