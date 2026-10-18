package loot

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/sim"
)

const dropTwoPi = float32(2 * math.Pi)

type PositionProjector interface {
	ProjectDropPosition(sim.Position) (sim.Position, bool)
}

// Placement follows 127 sub_9CF100/sub_9D0D30 (103 sub_9CA360/sub_9CBDE0).
// One emission retains shuffled slots across pickups, trying at most four rings.
type Placement struct {
	random    *sim.SimulatorRandom
	center    sim.Position
	radius    float32
	phase     float32
	angleStep float32
	slots     []uint32
	ringCount uint32
}

func NewPlacement(center sim.Position, footprint float32, random *sim.SimulatorRandom) (*Placement, error) {
	if random == nil || footprint < 0 || math.IsNaN(float64(footprint)) || math.IsInf(float64(footprint), 0) {
		return nil, errors.New("drop placement input invalid")
	}
	for _, coordinate := range []float32{center.X, center.Y, center.Z} {
		if math.IsNaN(float64(coordinate)) || math.IsInf(float64(coordinate), 0) {
			return nil, errors.New("drop placement center invalid")
		}
	}
	return &Placement{
		random: random, center: center, radius: max(float32(1), footprint*0.5),
		phase: float32(random.Float64() * float64(dropTwoPi)),
	}, nil
}

func (e *Placement) Next(projector PositionProjector) (sim.Position, error) {
	if e == nil || e.random == nil || projector == nil {
		return sim.Position{}, errors.New("drop placement unavailable")
	}
	for {
		if len(e.slots) == 0 {
			if e.ringCount == 4 {
				return e.center, nil
			}
			err := e.beginRing()
			if err != nil {
				return sim.Position{}, fmt.Errorf("placementRing: %w", err)
			}
		}
		last := len(e.slots) - 1
		angle := float32(e.slots[last])*e.angleStep + e.phase
		e.slots = e.slots[:last]
		if angle > dropTwoPi {
			angle -= dropTwoPi
		}
		// Native rotates +X about +Z through a half-angle quaternion.
		sine := float32(math.Sin(float64(angle) * 0.5))
		cosine := float32(math.Cos(float64(angle) * 0.5))
		radius := float32(e.random.Float64()*0.625 + float64(e.radius))
		candidate := sim.Position{
			X: e.center.X + (1-(sine*sine)*2)*radius,
			Y: e.center.Y + ((cosine*sine)*2)*radius,
			Z: e.center.Z,
		}
		projected, isFound := projector.ProjectDropPosition(candidate)
		if !isFound {
			continue
		}
		dx, dy := candidate.X-projected.X, candidate.Y-projected.Y
		if math.Sqrt(float64(dx*dx+dy*dy)) < 1 {
			return projected, nil
		}
	}
}

func (e *Placement) beginRing() error {
	e.radius += 2.5
	slotCount := int(min(float32(32), e.radius*dropTwoPi*0.5))
	e.angleStep = dropTwoPi / float32(slotCount)
	e.slots = make([]uint32, slotCount)
	for index := range e.slots {
		e.slots[index] = uint32(index)
	}
	for index := 1; index < len(e.slots); index++ {
		selected, err := e.random.Index(uint32(index + 1))
		if err != nil {
			return fmt.Errorf("slotShuffle[%d]: %w", index, err)
		}
		e.slots[index], e.slots[selected] = e.slots[selected], e.slots[index]
	}
	e.ringCount++
	return nil
}
