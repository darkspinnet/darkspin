package companion

import (
	"sort"

	"github.com/darkspinnet/darkspin/server/game"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type pursuitSelection struct {
	target       zonenpc.Snapshot
	distance     float32
	stopDistance float32
	destination  game.Vec3
	paths        []game.Vec3
}

// Keep the established distance/ID preference, but only reserve an ordinary
// pursuit after its route succeeds. A blocked nearer candidate must not hide
// a reachable alternative. Forced pursuit keeps its explicit route bypass.
func (e *Session) selectPursuit(actor Actor, targets []zonenpc.Snapshot, attackRange, aggroRadius float32, isForced bool) pursuitSelection {
	candidates := make([]pursuitSelection, 0)
	for _, target := range targets {
		if !zoneability.IsHostileTarget(target) || target.Plan.IsFixture {
			continue
		}
		stopDistance := attackRange + actor.ActorFootprintRadius() + max(float32(0), target.Plan.ActorFootprintRadius())
		distance := actor.Position.Sub(target.Plan.Position).Length()
		maximumRange := aggroRadius
		if !isForced && actor.AggroRadius > 0 {
			maximumRange = actor.acquisitionCenterRange(target.Plan.ActorFootprintRadius())
		}
		if distance <= stopDistance || distance > maximumRange {
			continue
		}
		candidates = append(candidates, pursuitSelection{target: target, distance: distance, stopDistance: stopDistance})
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].distance == candidates[right].distance {
			return candidates[left].target.Plan.ObjectID < candidates[right].target.Plan.ObjectID
		}
		return candidates[left].distance < candidates[right].distance
	})
	for _, candidate := range candidates {
		delta := actor.Position.Sub(candidate.target.Plan.Position)
		candidate.destination = candidate.target.Plan.Position.Add(delta.Scale(candidate.stopDistance / candidate.distance))
		if isForced {
			return candidate
		}
		points, destination, err := e.walkingPaths(actor, candidate.destination)
		if err != nil {
			continue
		}
		// Projection can find nearby terrain without finding an attackable
		// approach. Use the existing hit margin before accepting this route.
		if destination.Sub(candidate.target.Plan.Position).Length() > candidate.stopDistance+attackRangeTolerance {
			continue
		}
		candidate.paths = points
		candidate.destination = destination
		return candidate
	}
	return pursuitSelection{}
}
