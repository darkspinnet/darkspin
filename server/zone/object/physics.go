package object

import (
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
)

// NounPhysics is the transport-neutral collision and presentation metadata
// prepared for one authored noun.
type NounPhysics struct {
	FootprintRadius         float32
	BoundMinimum            sim.Position
	BoundMaximum            sim.Position
	CreatureType            uint32
	IsCreatureTypeKnown     bool
	OrdinaryDeathAnimation  string
	DanceAnimation          string
	Lifetime                time.Duration
	PickupTriggerHalfExtent sim.Position
}
