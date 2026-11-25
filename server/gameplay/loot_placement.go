package gameplay

import (
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneinteract "github.com/darkspinnet/darkspin/server/zone/interact"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

func (e *gameplayPeerSession) fixtureDNADropDestination(source sim.Position, destination sim.Position, clearance float32) sim.Position {
	player := navigation.Vec3(e.playerPosition)
	if float32Hypot(destination.X-player.X, destination.Y-player.Y) > campaignDNAPickupRadius {
		return destination
	}
	mesh := e.zone.Navigation()
	if mesh == nil {
		return destination
	}
	layer, isLayerFound := mesh.SelectLayerForMode(campaignSecurityBlitzFootprintFallback, uint8(e.binding.Mode))
	if !isLayerFound {
		return destination
	}
	options := navigation.ProjectionOptions{PlanLayer: layer, MaxDistance: zonenavigation.ProjectionDistance}
	playerProjection, err := mesh.Project(player, options)
	if err != nil {
		return destination
	}
	options.ComponentID = playerProjection.ComponentID
	options.IsComponentConstrained = true
	options.MaxDistance = 1
	// Try the far side of the breakable and then both sides of it. Keep the
	// original reachable landing when scenery blocks every alternate.
	toward := campaignDropDestination(source, e.playerPosition)
	radius := max(clearance+1, campaignDNAPickupRadius+1)
	dx := (toward.X - source.X) * radius / 2
	dy := (toward.Y - source.Y) * radius / 2
	for _, offset := range [3]navigation.Vec3{{X: -dx, Y: -dy}, {X: -dy, Y: dx}, {X: dy, Y: -dx}} {
		candidate := navigation.Vec3{X: source.X + offset.X, Y: source.Y + offset.Y, Z: source.Z}
		projection, projectErr := mesh.Project(candidate, options)
		if projectErr != nil {
			continue
		}
		land := projection.Position
		if float32Hypot(land.X-player.X, land.Y-player.Y) <= campaignDNAPickupRadius ||
			float32Hypot(land.X-source.X, land.Y-source.Y) < clearance {
			continue
		}
		return sim.Position(land)
	}
	return destination
}

// Sample around the source for every drop. A fixed point toward the player
// can land under the hero, and reusing it piles up equipment and catalysts.
func (e *gameplayPeerSession) separatedCampaignDropDestination(
	source sim.Position, destination sim.Position,
) sim.Position {
	if e == nil || e.zone == nil || e.zone.Pickups() == nil || e.zone.DropRandom() == nil {
		return destination
	}
	pickups := e.zone.Pickups().Snapshots()
	// Reuse the build-103 DropStuffForObject ring sampler rather than giving
	// every item and catalyst the same player-directed compatibility point.
	placement, err := zoneloot.NewPlacement(source, 0, e.zone.DropRandom())
	if err != nil {
		// Placement is cosmetic; retain the validated landing and its reward.
		return destination
	}
	projector := campaignDropProjector{mesh: e.zone.Navigation()}
	for attempt := 0; attempt < 32; attempt++ {
		candidate, placementErr := placement.Next(projector)
		if placementErr != nil {
			// A failed alternate must not discard a successfully rolled pickup.
			return destination
		}
		if isCampaignDropPositionClear(candidate, pickups) {
			return candidate
		}
	}
	// Keep the validated original destination if the local area has no room.
	return destination
}

func isCampaignDropPositionClear(destination sim.Position, pickups []zoneinteract.Pickup) bool {
	for _, pickup := range pickups {
		if pickup.Kind != zoneinteract.PickupEquipment && pickup.Kind != zoneinteract.PickupCrystal {
			continue
		}
		occupied := pickup.Position
		if !pickup.Flight.StartedAt.IsZero() {
			// New pickups still occupy their launch pose while in flight.
			// Reserve the landing now so the next drop cannot select it too.
			occupied = game.Vec3(pickup.Flight.Destination)
		}
		if math.Abs(float64(destination.Z-occupied.Z)) > 2 {
			continue
		}
		dx := destination.X - occupied.X
		dy := destination.Y - occupied.Y
		if dx*dx+dy*dy < 2.25 {
			return false
		}
	}
	return true
}
