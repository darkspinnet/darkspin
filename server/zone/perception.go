package zone

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// NPCInPerceptionCircle is the zone boundary for the recovered agent query.
// Owner resolution uses current object positions, independently of visibility,
// hostility, acquisition eligibility, or the owner's facing.
func (e *Zone) NPCInPerceptionCircle(objectID uint32, targetPosition game.Vec3, targetRadius float32) (bool, error) {
	if e == nil || e.NPCs() == nil {
		return false, errors.New("perception zone unavailable")
	}
	npc, isFound := e.NPCs().NPC(objectID)
	if !isFound {
		return false, fmt.Errorf("perceptionActor: %d unavailable", objectID)
	}
	director := e.DirectorDefinition()
	req := zonenpc.PerceptionQuery{
		ObjectID: objectID, TargetPosition: targetPosition, TargetRadius: targetRadius,
		LevelYawOverride: director.CameraYawOverride, IsLevelResolved: director.Level != "",
	}
	if npc.Plan.NPCProfile.IsPlayerPet && npc.Plan.OwnerObjectID != 0 {
		req.OwnerPosition, req.IsOwnerResolved = e.perceptionOwnerPosition(npc.Plan.OwnerObjectID)
	}
	isInside, err := e.NPCs().InPerceptionCircle(req)
	if err != nil {
		return false, fmt.Errorf("perceptionQuery: %w", err)
	}
	return isInside, nil
}

func (e *Zone) perceptionOwnerPosition(objectID uint32) (game.Vec3, bool) {
	if e.Hero() != nil {
		hero, isFound := e.Hero().SnapshotByObjectID(objectID)
		if isFound {
			return hero.Position, true
		}
	}
	if e.info.Companion != nil {
		companion, isFound := e.info.Companion.Snapshot(objectID)
		if isFound {
			return companion.Position, true
		}
	}
	owner, isFound := e.NPCs().NPC(objectID)
	if !isFound {
		return game.Vec3{}, false
	}
	return owner.Plan.Position, true
}
