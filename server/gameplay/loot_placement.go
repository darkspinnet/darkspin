package gameplay

import (
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneinteract "github.com/darkspinnet/darkspin/server/zone/interact"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
)

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
