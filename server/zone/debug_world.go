//go:build scenario

package zone

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
)

// ScenarioWorldState uses the zone and ordinary producer snapshot locks; it
// copies no resource definitions, account aggregates, or queued publications.
func (e *Zone) ScenarioWorldState(ctx context.Context, userID, generation uint64) (scenario.WorldState, error) {
	result := scenario.WorldState{}
	if e == nil {
		result.UnavailableReason = "zone is unavailable"
		return result, nil
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !e.mu.TryRLock() {
		select {
		case <-ctx.Done():
			return result, fmt.Errorf("worldZoneLock: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	defer e.mu.RUnlock()
	capturedAt := time.Now()
	result.CapturedAt = capturedAt.UTC()
	member, isMember := e.members[userID]
	if !isMember || member.PeerGeneration != generation {
		result.UnavailableReason = "zone membership is unavailable or changed"
		return result, nil
	}
	result.ZoneGeneration = e.generation
	result.IsMissionComplete = e.state == StateComplete
	result.IsZoneTerminal = e.state == StateComplete
	outcome := e.info.Outcome.ScenarioOutcome(outcomeMember(member))
	result.IsVictoryAvailable = outcome.IsAvailable
	result.IsVictoryCommitted = outcome.IsVictoryCommitted
	result.IsObjectiveComplete = e.isObjectiveComplete
	hero, isHero := e.info.Hero.ScenarioActor(userID, generation)
	if !isHero {
		result.UnavailableReason = "deployed hero snapshot is unavailable"
		return result, nil
	}
	result.Hero = &scenario.WorldHero{ObjectID: hero.ObjectID, Index: hero.CreatureIndex,
		Position: scenario.WorldPosition{X: hero.Position.X, Y: hero.Position.Y, Z: hero.Position.Z}, HitPoint: hero.HitPoint, ManaPoint: hero.ManaPoint,
		MaximumHitPoint: hero.MaximumHitPoint, MaximumManaPoint: hero.MaximumManaPoint, IsDefeated: hero.HitPoint <= 0}
	result.Pickups = []scenario.WorldPickup{}
	if e.startedAt.IsZero() || capturedAt.Before(e.startedAt) {
		result.PickupUnavailableReason = "zone simulation clock is unavailable"
	} else {
		pickups, pickupErr := e.info.Orbs.ScenarioPickups(ctx, e.info.Pickups, result.Hero.Position, capturedAt, capturedAt.Sub(e.startedAt))
		if pickupErr != nil {
			return result, fmt.Errorf("worldPickup: %w", pickupErr)
		}
		result.Pickups = pickups.Pickups
		result.PickupTotal = pickups.Total
		result.IsPickupTruncated = pickups.IsTruncated
		result.IsPickupAvailable = pickups.IsAvailable
		result.PickupUnavailableReason = pickups.UnavailableReason
	}
	npcs, total, err := e.info.NPCs.ScenarioNearby(ctx, result.Hero.Position)
	if err != nil {
		return result, fmt.Errorf("worldNPC: %w", err)
	}
	result.NPCs = npcs
	result.NPCTotal = total
	result.IsNPCTruncated = total > len(npcs)
	if e.info.Boss != nil {
		result.Boss = e.info.Boss.ScenarioBoss()
	}
	routes := e.info.DirectorDefinition.TeleportRoutes()
	result.IsTeleporterTruncated = len(routes) > 128
	for index, route := range routes {
		if index >= 128 {
			break
		}
		result.Teleporters = append(result.Teleporters, scenario.WorldTeleporter{RouteIndex: -1, MarkerID: route.MarkerID, DestinationMarkerID: route.DestinationMarkerID,
			Source: scenario.WorldPosition{X: route.Source.X, Y: route.Source.Y, Z: route.Source.Z}, Destination: scenario.WorldPosition{X: route.Destination.X, Y: route.Destination.Y, Z: route.Destination.Z},
			IsSecurity: route.IsSecurity, IsBoss: route.IsBoss, IsTunnel: route.IsTunnel})
	}
	result.IsAvailable = true
	return result, nil
}
