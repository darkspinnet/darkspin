package zone

import (
	"errors"
	"fmt"
	"time"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// AdvanceMutationAgents takes one shared view of controlled actors before the
// NPC feature advances its source and target owned Lua lifetimes. Stealth does
// not remove a controlled object from the authored Wander proximity scan.
func (e *Zone) AdvanceMutationAgents(at time.Time) ([]zonenpc.MutationEvent, error) {
	if e == nil {
		return nil, errors.New("nil campaign zone")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return nil, nil
	}
	heroes := e.info.Hero.Snapshots()
	controlledTargets := make([]zonenpc.Target, 0, len(heroes))
	for _, actor := range heroes {
		member, isFound := e.members[actor.UserID]
		if !isFound || !member.IsConnected || member.PeerGeneration != actor.PeerGeneration {
			continue
		}
		controlledTargets = append(controlledTargets, zonenpc.Target{
			ObjectID: actor.ObjectID, Position: actor.Position,
			FootprintRadius: actor.FootprintRadius,
			Faction:         zonenpc.FactionPlayerAligned, IsAlive: actor.HitPoint > 0,
			Owner: zonenpc.ActionOwner{UserID: actor.UserID, PeerGeneration: actor.PeerGeneration},
		})
	}
	events, err := e.info.NPCs.AdvanceMutationAgents(zonenpc.MutationAdvanceRequest{
		At: at, ControlledTargets: controlledTargets, Targets: e.livePlayerAlignedTargets(),
	})
	if err != nil {
		return events, fmt.Errorf("mutationAdvance: %w", err)
	}
	return events, nil
}
