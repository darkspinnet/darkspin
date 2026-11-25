package zone

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/squad"
	zonecheckpoint "github.com/darkspinnet/darkspin/server/zone/checkpoint"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type CrystalInventoryChange struct {
	UserID         uint64
	PeerGeneration uint64
	PickupObjectID uint32
	Inventory      sim.CrystalInventory
	Roster         game.GameplayRoster
	Squad          squad.State
	Position       game.Vec3
}

// CommitCrystalInventory keeps catalyst capacities, current resources and the
// resume checkpoint together. The caller has already staged and validated its
// complete resource projection before entering this operation.
func (e *Zone) CommitCrystalInventory(req CrystalInventoryChange) error {
	if e == nil || req.UserID == 0 || req.PeerGeneration == 0 ||
		!zonegeometry.IsFinite(req.Position) {
		return errors.New("crystal inventory change invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	member, isFound := e.members[req.UserID]
	if !isFound || member.PeerGeneration != req.PeerGeneration {
		return errors.New("crystal inventory member unavailable")
	}
	actor, isActorFound := e.info.Hero.Snapshot(req.UserID, req.PeerGeneration)
	if isActorFound {
		if actor.CreatureIndex >= squad.Size {
			return errors.New("crystal hero index invalid")
		}
		previous := actor
		character := req.Squad.Characters[actor.CreatureIndex]
		creature := req.Roster.Creatures[actor.CreatureIndex]
		actor.HitPoint = character.HitPoints
		actor.ManaPoint = character.ManaPoints
		actor.MaximumHitPoint = creature.MaximumHitPoint
		actor.MaximumManaPoint = creature.MaximumPowerPoint
		err := e.info.Hero.Put(actor)
		if err != nil {
			return fmt.Errorf("crystalHero: %w", err)
		}
		if req.PickupObjectID != 0 && !e.info.Pickups.Commit(req.PickupObjectID) {
			rollbackErr := e.info.Hero.Put(previous)
			return fmt.Errorf("crystalPickup: %w", errors.Join(errors.New("reservation missing"), rollbackErr))
		}
	} else if req.PickupObjectID != 0 && !e.info.Pickups.Commit(req.PickupObjectID) {
		return errors.New("crystal pickup reservation missing")
	}
	e.crystals[req.UserID] = req.Inventory
	member.Roster = req.Roster
	e.members[req.UserID] = member
	e.checkpointSquads[req.UserID] = zonecheckpoint.Squad{
		UserID: req.UserID, Position: req.Position, State: req.Squad,
	}
	return nil
}
