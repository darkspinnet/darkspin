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
// unpublished actors other than explicitly retired controlled actors. It does
// not assert per-client native visibility.
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
	total := 0
	for _, actor := range e.npcs {
		err := ctx.Err()
		if err != nil {
			return nil, 0, fmt.Errorf("nearbyScan: %w", err)
		}
		if actor.IsScenarioControlled() && actor.Plan.scenarioControl.lastCommand == "retire" {
			continue
		}
		total++
		position := actor.Plan.Position
		dx, dy, dz := float64(position.X-center.X), float64(position.Y-center.Y), float64(position.Z-center.Z)
		npc := scenario.WorldNPC{ObjectID: actor.Plan.ObjectID, Noun: actor.Plan.NounName,
			Position: scenario.WorldPosition{X: position.X, Y: position.Y, Z: position.Z},
			HitPoint: actor.HitPoint, IsDead: actor.IsDefeated, IsPublished: actor.IsPublished,
			IsHostile: actor.Faction == FactionNonPlayerAligned, IsFixture: actor.Plan.IsFixture, IsBoss: actor.Plan.IsBoss,
			IsHostilityAvailable: actor.Faction != FactionUnknown,
			MutationPhase:        scenarioMutationPhase(actor),
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
	return npcs, total, nil
}

func scenarioMutationPhase(actor Snapshot) string {
	if actor.IsDefeated || !actor.IsPublished {
		return ""
	}
	if actor.Mutation.Transform.IsActive {
		return "growing"
	}
	if actor.Mutation.Transform.SourceObjectID != 0 && actor.Plan.IsEliteModifierApplied() {
		return "promoted"
	}
	if !actor.Mutation.IsPassiveActive {
		return ""
	}
	beam := actor.Mutation.Beam
	switch {
	case beam.IsActive && beam.IsChannelActive:
		return "channel"
	case beam.IsActive && beam.IsModelActive:
		return "firing"
	case beam.IsActive:
		return "windup"
	default:
		return "idle"
	}
}
