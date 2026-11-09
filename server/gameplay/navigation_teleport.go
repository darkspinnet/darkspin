package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/zone"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// Actor navigation reports a discontinuous gate traversal separately from its
// walking waypoints. Publish the actual landing rather than interpolating the
// client through a closed fence. The caller holds the registry lock.
func (e *gameplaySessionRegistry) queueNavigationTeleportsLocked(world *zone.Zone) error {
	if world == nil {
		return nil
	}
	packets := make([][]byte, 0)
	for _, traversal := range world.NPCs().DrainNavigationTeleports() {
		actor, isFound := world.NPCs().NPC(traversal.ObjectID)
		if !isFound || actor.IsDefeated {
			continue
		}
		packet, err := marshalNavigationTeleport(traversal.ObjectID, actor.Plan.Position, actor.Facing)
		if err != nil {
			return fmt.Errorf("npcTraversal: %w", err)
		}
		packets = append(packets, packet)
	}
	for _, traversal := range world.Companion().DrainNavigationTeleports() {
		actor, isFound := world.Companion().Snapshot(traversal.ObjectID)
		if !isFound || actor.HitPoint <= 0 {
			continue
		}
		packet, err := marshalNavigationTeleport(traversal.ObjectID, actor.Position, actor.Facing)
		if err != nil {
			return fmt.Errorf("companionTraversal: %w", err)
		}
		packets = append(packets, packet)
		destination, isMoving := actor.NavigationDestination()
		if isMoving && destination != actor.Position {
			movementPackets, movementErr := npcraknet.PursuitWaypoint(actor.ObjectID, destination)
			if movementErr != nil {
				return fmt.Errorf("companionResume: %w", movementErr)
			}
			packets = append(packets, movementPackets...)
		}
	}
	if len(packets) != 0 {
		e.queueChannelPresentationLocked(world, packets)
	}
	return nil
}

func marshalNavigationTeleport(objectID uint32, position, facing game.Vec3) ([]byte, error) {
	packet, err := npcraknet.RestorePose(objectID, position, facing)
	if err != nil {
		return nil, fmt.Errorf("traversalMarshal: %w", err)
	}
	return packet, nil
}
