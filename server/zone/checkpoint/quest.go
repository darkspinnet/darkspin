package checkpoint

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/util"
	zoneobjective "github.com/darkspinnet/darkspin/server/zone/objective"
	zonequest "github.com/darkspinnet/darkspin/server/zone/quest"
)

func validateQuestRequest(req zoneobjective.DropRequest) error {
	if req.ID == 0 || req.ObjectiveID != zoneobjective.LootCrystalsID ||
		req.NounID != util.HashID(zonequest.CrystalNoun) || req.SourceObjectID == 0 || req.SourceFootprintRadius < 0 {
		return errors.New("quest request identity invalid")
	}
	for _, scalar := range []float32{req.SourcePosition.X, req.SourcePosition.Y, req.SourcePosition.Z, req.SourceFootprintRadius} {
		if math.IsNaN(float64(scalar)) || math.IsInf(float64(scalar), 0) {
			return errors.New("quest request source invalid")
		}
	}
	return nil
}

func validateQuestCrystals(snapshot Snapshot) error {
	err := zoneobjective.ValidateDropState(zoneobjective.Checkpoint{Objectives: snapshot.Objectives,
		Runtimes: snapshot.ObjectiveRuntimes, Drops: snapshot.ObjectiveDrops, Events: snapshot.ObjectiveEvents,
		NextDropID: snapshot.ObjectiveNextDropID})
	if err != nil {
		return fmt.Errorf("questOutboxState: %w", err)
	}
	err = zonequest.Validate(snapshot.QuestCrystals)
	if err != nil {
		return fmt.Errorf("questWorld: %w", err)
	}
	requests := make(map[uint64]zoneobjective.DropRequest)
	objects := make(map[uint32]bool)
	for _, npc := range snapshot.NPCs {
		objects[npc.State.Plan.ObjectID] = true
	}
	for _, hero := range snapshot.Heroes {
		objects[hero.ObjectID] = true
	}
	for _, inventory := range snapshot.MissionEquipments {
		for _, equipment := range inventory.Equipments {
			objects[equipment.ObjectID] = true
		}
	}
	for _, crystal := range snapshot.QuestCrystals {
		err = validateQuestRequest(crystal.Request)
		if err != nil {
			return fmt.Errorf("questRecord: %w", err)
		}
		if objects[crystal.ObjectID] {
			return errors.New("quest object collision")
		}
		objects[crystal.ObjectID] = true
		requests[crystal.Request.ID] = crystal.Request
	}
	previousID := uint64(0)
	for _, req := range snapshot.ObjectiveDrops {
		err = validateQuestRequest(req)
		if err != nil {
			return fmt.Errorf("questOutbox: %w", err)
		}
		if req.ID <= previousID {
			return errors.New("quest outbox order invalid")
		}
		previousID = req.ID
		if retained, isFound := requests[req.ID]; isFound && retained != req {
			return errors.New("quest transfer mismatch")
		}
		requests[req.ID] = req
	}
	if snapshot.ObjectiveNextDropID != uint64(len(requests))+1 {
		return errors.New("quest emission ownership incomplete")
	}
	for id := range requests {
		if id >= snapshot.ObjectiveNextDropID {
			return errors.New("quest emission highwater invalid")
		}
	}
	if len(requests) == 0 && len(snapshot.ObjectiveEvents) == 0 {
		return nil
	}
	for _, runtime := range snapshot.ObjectiveRuntimes {
		if runtime.ObjectiveID == zoneobjective.LootCrystalsID {
			return nil
		}
	}
	return errors.New("quest objective runtime missing")
}
