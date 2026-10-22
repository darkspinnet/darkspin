package zone

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneinteract "github.com/darkspinnet/darkspin/server/zone/interact"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zonequest "github.com/darkspinnet/darkspin/server/zone/quest"
)

func (e *Zone) QuestCrystals() *zonequest.Session { return e.questCrystal }

func questObjectiveTeam(faction zonenpc.Faction) uint8 {
	if faction == zonenpc.FactionPlayerAligned {
		return 1
	}
	if faction == zonenpc.FactionNonPlayerAligned {
		return 2
	}
	return 0
}

func (e *Zone) questDropSourcePosition(npc zonenpc.Snapshot) sim.Position {
	source := sim.Position(npc.Plan.Position)
	if e.info.Navigation == nil {
		return source
	}
	layer := uint8(0)
	if npc.Navigation.IsPresent {
		layer = npc.Navigation.PlanLayer
	}
	projection, err := e.info.Navigation.Project(navigation.Vec3(source), navigation.ProjectionOptions{
		PlanLayer: layer, MaxDistance: zonenavigation.ProjectionDistance})
	if err != nil {
		// Native ignores source-projection failure. Keep the captured raw
		// source instead of selecting another layer or inventing a position.
		return source
	}
	return sim.Position(projection.Position)
}

// Ownership transfers before acknowledgement. Retries reuse the accepted
// object and destination, including when a checkpoint retains both owners.
func (e *Zone) PrepareQuestCrystals(ctx context.Context, projector zoneloot.PositionProjector, timestamp uint64) error {
	e.questMutex.Lock()
	defer e.questMutex.Unlock()
	if !e.IsActive() || e.info.Objective == nil {
		return nil
	}
	updates, retryErr := e.info.Objective.RetryDrops(ctx)
	e.PublishObjective(updates)
	for _, req := range e.info.Objective.PendingDrops() {
		crystal, isFound := e.questCrystal.Request(req.ID)
		if !isFound {
			objectID, err := e.ReserveObjectIDs(1)
			if err != nil {
				return fmt.Errorf("questObject: %w", err)
			}
			placement, err := zoneloot.NewPlacement(req.SourcePosition, req.SourceFootprintRadius, e.info.DropRandom)
			if err != nil {
				return fmt.Errorf("questPlacement: %w", err)
			}
			destination, err := placement.Next(projector)
			if err != nil {
				return fmt.Errorf("questDestination: %w", err)
			}
			crystal, err = e.questCrystal.Admit(req, objectID, destination, time.Now(), timestamp)
			if err != nil {
				return fmt.Errorf("questAdmit: %w", err)
			}
		}
		if !crystal.IsCollected {
			err := e.registerQuestCrystal(crystal)
			if err != nil {
				return fmt.Errorf("questRegister: %w", err)
			}
		}
		err := e.info.Objective.AckDrop(req.ID)
		if err != nil {
			return fmt.Errorf("questAcknowledge: %w", err)
		}
	}
	if retryErr != nil {
		return fmt.Errorf("questRetry: %w", retryErr)
	}
	return nil
}

func (e *Zone) registerQuestCrystal(crystal zonequest.Crystal) error {
	if _, isFound := e.info.Pickups.Pickup(crystal.ObjectID); isFound {
		return nil
	}
	err := e.info.Pickups.Register(zoneinteract.Pickup{ObjectID: crystal.ObjectID,
		Kind: zoneinteract.PickupQuestCrystal, Position: game.Vec3(crystal.Flight.Position),
		SourcePosition: game.Vec3(crystal.Flight.Source), IsSourcePositionKnown: true,
		Flight: crystal.Flight})
	if err != nil {
		return fmt.Errorf("questPickup: %w", err)
	}
	return nil
}

func (e *Zone) CollectQuestCrystal(ctx context.Context, claim zonequest.Claim) (bool, error) {
	e.questMutex.Lock()
	defer e.questMutex.Unlock()
	if !e.IsActive() {
		return false, nil
	}
	isCollected, err := e.questCrystal.Collect(ctx, claim, e)
	if err != nil {
		return false, fmt.Errorf("questCollect: %w", err)
	}
	if isCollected {
		e.info.Pickups.Remove(claim.ObjectID)
	}
	return isCollected, nil
}

func (e *Zone) restoreQuestCrystals(states []zonequest.State) error {
	err := e.questCrystal.Restore(states, time.Now())
	if err != nil {
		return fmt.Errorf("questRestore: %w", err)
	}
	for _, state := range states {
		if state.IsCollected {
			continue
		}
		crystal, isFound := e.questCrystal.Crystal(state.ObjectID, time.Now())
		if !isFound {
			continue
		}
		err = e.registerQuestCrystal(crystal)
		if err != nil {
			return fmt.Errorf("questRestorePickup: %w", err)
		}
	}
	return nil
}
