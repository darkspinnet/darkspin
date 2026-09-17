package loot

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
)

type EquipmentPlanInput struct {
	ObjectID       uint32
	Rarity         Rarity
	Source         sim.Position
	Destination    sim.Position
	SimulationTime time.Duration
}

type EquipmentPlan struct {
	ObjectID    uint32
	NounName    string
	Source      sim.Position
	Destination sim.Position
	Lob         sim.CrystalLob
}

func PlanEquipment(input EquipmentPlanInput) (EquipmentPlan, error) {
	if input.ObjectID == 0 {
		return EquipmentPlan{}, errors.New("invalid equipment object")
	}
	lob, err := sim.BuildDropLob(input.SimulationTime, input.Source, input.Destination)
	if err != nil {
		return EquipmentPlan{}, fmt.Errorf("equipmentLob: %w", err)
	}
	return EquipmentPlan{
		ObjectID: input.ObjectID, NounName: EquipmentContainerNoun(input.Rarity),
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
	return DNAPlan{
		ObjectID: input.ObjectID, Amount: input.Amount,
		Source: input.Source, Destination: input.Destination, Lob: lob,
	}, nil
}

type CrystalPlanInput struct {
	Challenge   int32
	ChanceScale float32
	RandomDraw  uint32
	World       sim.CrystalDropInput
}

func PlanCrystal(input CrystalPlanInput) (sim.CrystalPickupRequest, bool, error) {
	isDrop, err := IsCrystalDrop(input.Challenge, input.ChanceScale, input.RandomDraw)
	if err != nil {
		return sim.CrystalPickupRequest{}, false, fmt.Errorf("crystalDecision: %w", err)
	}
	if !isDrop {
		return sim.CrystalPickupRequest{}, false, nil
	}
	request, err := sim.BuildCrystalDropWorldRequest(input.World)
	if err != nil {
		return sim.CrystalPickupRequest{}, false, fmt.Errorf("crystalWorld: %w", err)
	}
	if len(request.Pickups) != 1 {
		return sim.CrystalPickupRequest{}, false, fmt.Errorf("crystalCount: %d", len(request.Pickups))
	}
	return request.Pickups[0], true, nil
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
