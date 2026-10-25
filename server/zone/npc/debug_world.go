//go:build scenario

package npc

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
)

// ScenarioNearby copies at most 128 scalar actors, including dead and
// unpublished actors. It does not assert per-client native visibility.
func (e *Session) ScenarioNearby(ctx context.Context, center scenario.WorldPosition) ([]scenario.WorldNPC, int, error) {
	if e == nil {
		return nil, 0, nil
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !e.mu.TryRLock() {
		select {
		case <-ctx.Done():
			return nil, 0, fmt.Errorf("nearbyLock: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	defer e.mu.RUnlock()
	npcs := make([]scenario.WorldNPC, 0, 128)
	for _, actor := range e.npcs {
		err := ctx.Err()
		if err != nil {
			return nil, 0, fmt.Errorf("nearbyScan: %w", err)
		}
		position := actor.Plan.Position
		dx, dy, dz := float64(position.X-center.X), float64(position.Y-center.Y), float64(position.Z-center.Z)
		npc := scenario.WorldNPC{ObjectID: actor.Plan.ObjectID, Noun: actor.Plan.NounName,
			Position: scenario.WorldPosition{X: position.X, Y: position.Y, Z: position.Z},
			HitPoint: actor.HitPoint, IsDead: actor.IsDefeated, IsPublished: actor.IsPublished,
			IsHostile: actor.Faction == FactionNonPlayerAligned, IsFixture: actor.Plan.IsFixture, IsBoss: actor.Plan.IsBoss,
			IsHostilityAvailable: actor.Faction != FactionUnknown,
			DistanceSquared:      dx*dx + dy*dy + dz*dz}
		index := sort.Search(len(npcs), func(index int) bool {
			return npcs[index].DistanceSquared > npc.DistanceSquared || npcs[index].DistanceSquared == npc.DistanceSquared && npcs[index].ObjectID >= npc.ObjectID
		})
		if index >= 128 {
			continue
		}
		if len(npcs) < 128 {
			npcs = append(npcs, scenario.WorldNPC{})
		}
		copy(npcs[index+1:], npcs[index:len(npcs)-1])
		npcs[index] = npc
	}
	return npcs, len(e.npcs), nil
}
