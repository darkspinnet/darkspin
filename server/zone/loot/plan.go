package loot

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
)

type EquipmentPlanInput struct {
	ObjectID           uint32
	Rarity             Rarity
	PresentationPolicy EquipmentPresentationPolicy
	Source             sim.Position
	Destination        sim.Position
	SimulationTime     time.Duration
}

type EquipmentPlan struct {
	ObjectID           uint32
	NounName           string
	PresentationPolicy EquipmentPresentationPolicy
	Source             sim.Position
	Destination        sim.Position
	Lob                sim.CrystalLob
}

func PlanEquipment(input EquipmentPlanInput) (EquipmentPlan, error) {
	if input.ObjectID == 0 {
		return EquipmentPlan{}, errors.New("invalid equipment object")
	}
	nounName, isSupported := input.PresentationPolicy.ContainerNoun(input.Rarity)
	if !isSupported {
		return EquipmentPlan{}, errors.New("unsupported equipment presentation rarity")
	}
	lob, err := sim.BuildDropLob(input.SimulationTime, input.Source, input.Destination)
	if err != nil {
		return EquipmentPlan{}, fmt.Errorf("equipmentLob: %w", err)
	}
	lob.IsGroundCollisionOnly = true
	return EquipmentPlan{
		ObjectID: input.ObjectID, NounName: nounName, PresentationPolicy: input.PresentationPolicy,
		Source: input.Source, Destination: input.Destination, Lob: lob,
	}, nil
}

type DNAPlanInput struct {
	ObjectID       uint32
	Amount         uint32
	Source         sim.Position
	Destination    sim.Position
	SimulationTime time.Duration
}

type DNAPlan struct {
	ObjectID    uint32
	Amount      uint32
	Source      sim.Position
	Destination sim.Position
	Lob         sim.CrystalLob
}

func PlanDNA(input DNAPlanInput) (DNAPlan, error) {
	if input.ObjectID == 0 || input.Amount == 0 {
		return DNAPlan{}, errors.New("invalid DNA drop")
	}
	lob, err := sim.BuildDropLob(input.SimulationTime, input.Source, input.Destination)
	if err != nil {
		return DNAPlan{}, fmt.Errorf("dnaLob: %w", err)
	}
	lob.IsGroundCollisionOnly = true
	return DNAPlan{
		ObjectID: input.ObjectID, Amount: input.Amount,
		Source: input.Source, Destination: input.Destination, Lob: lob,
	}, nil
}

type CrystalPlanInput struct {
	Challenge       int32
	ChanceScale     float32
	RandomDraw      uint32
	ChanceThreshold float32
	// An explicit override permits zero percentage points without falling
	// back to the native challenge-derived chance.
	IsChanceThresholdOverridden bool
	World                       sim.CrystalDropInput
}

func PlanCrystal(input CrystalPlanInput) (sim.CrystalPickupRequest, bool, error) {
	if !sim.IsCrystalDropStageEligible(input.World.CurrentDifficulty, input.World.MinimumDifficulty) {
		return sim.CrystalPickupRequest{}, false, nil
	}
	isDrop := false
	if input.IsChanceThresholdOverridden {
		if input.ChanceThreshold < 0 || input.ChanceThreshold > 100 ||
			math.IsNaN(float64(input.ChanceThreshold)) || math.IsInf(float64(input.ChanceThreshold), 0) ||
			input.RandomDraw >= 100 {
			return sim.CrystalPickupRequest{}, false, errors.New("invalid crystal chance override")
		}
		isDrop = input.RandomDraw < uint32(input.ChanceThreshold)
	} else {
		var err error
		isDrop, err = IsCrystalDrop(input.Challenge, input.ChanceScale, input.RandomDraw)
		if err != nil {
			return sim.CrystalPickupRequest{}, false, fmt.Errorf("crystalDecision: %w", err)
		}
	}
	if !isDrop {
		return sim.CrystalPickupRequest{}, false, nil
	}
	world := input.World
	if world.PlayerCount != 1 || len(world.PickupRoles) != 1 || len(world.Destinations) != 1 ||
		world.PlayerRole == "" || world.SourceRole == "" || world.ControlledRole == "" {
		return sim.CrystalPickupRequest{}, false, errors.New("invalid ordinary crystal world input")
	}
	pickup, isSelected, err := sim.SelectCrystalPickup(sim.CrystalSelectionInput{
		Stage: world.CurrentDifficulty, MinorStageCount: world.MinorStageCount, Role: world.PickupRoles[0],
		SimulationTime: world.SimulationTime,
		SourcePosition: world.SourcePosition, Destination: world.Destinations[0],
		Definitions: world.Definitions, LevelOffsets: world.LevelOffsets,
		RecentTypes: world.RecentTypes, RarityMisses: world.RarityMisses,
		Random: world.Random,
	})
	if err != nil {
		return sim.CrystalPickupRequest{}, false, fmt.Errorf("crystalSelect: %w", err)
	}
	return pickup, isSelected, nil
}

func PlanOrb(input sim.OrbDropInput) (sim.OrbPickupRequest, bool, error) {
	request, err := sim.BuildOrbDropWorldRequest(input)
	if err != nil {
		return sim.OrbPickupRequest{}, false, fmt.Errorf("orbWorld: %w", err)
	}
	if len(request.Pickups) > 1 {
		return sim.OrbPickupRequest{}, false, fmt.Errorf("orbCount: %d", len(request.Pickups))
	}
	if len(request.Pickups) == 0 {
		return sim.OrbPickupRequest{}, false, nil
	}
	return request.Pickups[0], true, nil
}

// PlanOrbs retains every pickup accepted by the native budget loop.
func PlanOrbs(req sim.OrbDropInput) ([]sim.OrbPickupRequest, error) {
	request, err := sim.BuildOrbDropWorldRequest(req)
	if err != nil {
		return nil, fmt.Errorf("orbsWorld: %w", err)
	}
	return request.Pickups, nil
}
