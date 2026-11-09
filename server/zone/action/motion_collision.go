package action

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/sim"
)

// Recheck only the portion of the retained route consumed by this observation.
// Following its corners avoids testing a chord through a legitimate path bend.
// The caller holds the motion lock and owns the supplied movement copy.
func (e *Motion) advanceClearPathLocked(movement *sim.LinearMovement, at time.Duration) (sim.Position, bool, error) {
	snapshot := movement.Snapshot()
	if e.navigation == nil || !snapshot.IsMoving || at <= snapshot.At {
		position, err := movement.Position(at)
		if err != nil {
			return sim.Position{}, false, fmt.Errorf("clearAdvance: %w", err)
		}
		return position, false, nil
	}
	planLayer, isLayerFound := e.navigation.ActorLayer(e.footprintRadius, e.navigationActor)
	if !isLayerFound {
		return sim.Position{}, false, fmt.Errorf("clearLayer: actor navigation unavailable")
	}
	travel := snapshot.Speed * float32(at-snapshot.At) / float32(time.Second)
	start := snapshot.Position
	for _, corner := range movement.RemainingPath() {
		delta := sim.Position{X: corner.X - start.X, Y: corner.Y - start.Y, Z: corner.Z - start.Z}
		distance := float32(math.Sqrt(float64(delta.X*delta.X + delta.Y*delta.Y + delta.Z*delta.Z)))
		end := corner
		if distance > travel {
			scale := travel / distance
			end = sim.Position{X: start.X + delta.X*scale, Y: start.Y + delta.Y*scale, Z: start.Z + delta.Z*scale}
		}
		if !e.navigation.IsSegmentClear(navigation.Vec3(start), navigation.Vec3(end), planLayer) {
			position := e.lastClearPathPositionLocked(start, end, planLayer)
			err := movement.Teleport(at, position)
			if err != nil {
				return sim.Position{}, false, fmt.Errorf("clearStop: %w", err)
			}
			return position, true, nil
		}
		if distance >= travel {
			break
		}
		travel -= distance
		start = corner
	}
	position, err := movement.Position(at)
	if err != nil {
		return sim.Position{}, false, fmt.Errorf("clearPosition: %w", err)
	}
	return position, false, nil
}

// Keep the stopped pose on the clear side of the first newly blocked segment.
// These bounded overlay checks never perform projection or a new path search.
func (e *Motion) lastClearPathPositionLocked(start, end sim.Position, planLayer uint8) sim.Position {
	clearFraction, blockedFraction := float32(0), float32(1)
	position := start
	for index := 0; index < 20; index++ {
		fraction := (clearFraction + blockedFraction) / 2
		candidate := sim.Position{X: start.X + (end.X-start.X)*fraction,
			Y: start.Y + (end.Y-start.Y)*fraction, Z: start.Z + (end.Z-start.Z)*fraction}
		if e.navigation.IsSegmentClear(navigation.Vec3(start), navigation.Vec3(candidate), planLayer) {
			clearFraction = fraction
			position = candidate
		} else {
			blockedFraction = fraction
		}
	}
	return position
}
