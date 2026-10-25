//go:build scenario

package interact

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/sim"
)

// ScenarioPickups joins both actual owners without reserving a pickup,
// interpolating a persistent flight cache or consuming an orb. The gameplay
// registry caller pins the world epoch; local contention reports unknown.
func (e *OrbRegistry) ScenarioPickups(ctx context.Context, pickupRegistry *PickupRegistry, center scenario.WorldPosition, now time.Time, elapsed time.Duration) (scenario.WorldPickupSnapshot, error) {
	result := scenario.WorldPickupSnapshot{Pickups: []scenario.WorldPickup{}}
	err := ctx.Err()
	if err != nil {
		return result, fmt.Errorf("pickupSnapshotContext: %w", err)
	}
	if e == nil || pickupRegistry == nil {
		result.UnavailableReason = "resource-orb registry is unavailable"
		return result, nil
	}
	if !e.mu.TryLock() {
		result.UnavailableReason = "resource-orb registry is busy"
		return result, nil
	}
	defer e.mu.Unlock()
	if !pickupRegistry.mu.TryLock() {
		result.UnavailableReason = "pickup lifecycle registry is busy"
		return result, nil
	}
	defer pickupRegistry.mu.Unlock()
	for _, orb := range e.orbs {
		err = ctx.Err()
		if err != nil {
			return scenario.WorldPickupSnapshot{}, fmt.Errorf("pickupSnapshotScan: %w", err)
		}
		kind := "health"
		if orb.Request.Kind == sim.ManaOrbDrop {
			kind = "mana"
		} else if orb.Request.Kind != sim.HealthOrbDrop {
			continue
		}
		pickup, isFound := pickupRegistry.pickups[orb.ObjectID]
		if !isFound || pickup.Kind != PickupOrb || pickupRegistry.set == nil {
			return scenario.WorldPickupSnapshot{Pickups: []scenario.WorldPickup{}, UnavailableReason: "resource-orb registration does not match pickup lifecycle"}, nil
		}
		isReserved := pickupRegistry.set.IsReserved(orb.ObjectID)
		isRegistered := pickupRegistry.set.IsAvailable(orb.ObjectID) || isReserved
		if !isRegistered {
			return scenario.WorldPickupSnapshot{Pickups: []scenario.WorldPickup{}, UnavailableReason: "resource-orb lifecycle registration is unavailable"}, nil
		}
		// Contacts uses this destination, after AvailableAt, rather than the
		// native projectile's rendered or interpolated position.
		position := scenario.WorldPosition{X: orb.Request.Destination.X, Y: orb.Request.Destination.Y, Z: orb.Request.Destination.Z}
		dx, dy, dz := float64(position.X-center.X), float64(position.Y-center.Y), float64(position.Z-center.Z)
		availableIn := orb.AvailableAt.Sub(now)
		if orb.AvailableAt.IsZero() || availableIn < 0 {
			availableIn = 0
		}
		expiresIn := pickup.ExpiresAt - elapsed
		isExpired := pickup.ExpiresAt > 0 && expiresIn <= 0
		if expiresIn < 0 {
			expiresIn = 0
		}
		// The ordinary collectCampaignOrbs contact sphere is 2.5 world units.
		row := scenario.WorldPickup{ObjectID: orb.ObjectID, Kind: kind, Position: position, Destination: position,
			AvailableInMS: pickupDurationMS(availableIn), ExpiresInMS: pickupDurationMS(expiresIn),
			IsCollectible: isRegistered && !isReserved && !isExpired && availableIn == 0,
			IsReserved:    isReserved, IsExpired: isExpired, IsServerRegistered: isRegistered,
			DistanceSquared: dx*dx + dy*dy + dz*dz, PickupRadius: 2.5}
		result.Total++
		index := sort.Search(len(result.Pickups), func(index int) bool {
			candidate := result.Pickups[index]
			return candidate.DistanceSquared > row.DistanceSquared || candidate.DistanceSquared == row.DistanceSquared && candidate.ObjectID >= row.ObjectID
		})
		if index >= 64 {
			continue
		}
		if len(result.Pickups) < 64 {
			result.Pickups = append(result.Pickups, scenario.WorldPickup{})
		}
		copy(result.Pickups[index+1:], result.Pickups[index:len(result.Pickups)-1])
		result.Pickups[index] = row
	}
	result.IsAvailable = true
	result.IsTruncated = result.Total > len(result.Pickups)
	return result, nil
}

// Round positive deadlines upward so a sub-millisecond wait is not advertised
// as available/expired merely because it was encoded in integer milliseconds.
func pickupDurationMS(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return int64((duration-1)/time.Millisecond) + 1
}
