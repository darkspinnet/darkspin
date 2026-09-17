package interact

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type Orb struct {
	ObjectID    uint32
	Request     sim.OrbPickupRequest
	AvailableAt time.Time
}

type OrbRegistry struct {
	mu   sync.Mutex
	orbs map[uint32]Orb
}

func NewOrbRegistry() *OrbRegistry {
	return &OrbRegistry{orbs: make(map[uint32]Orb)}
}

func (r *OrbRegistry) Add(orb Orb) error {
	if r == nil || orb.ObjectID == 0 || orb.Request.Role == "" {
		return errors.New("invalid campaign orb")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, isFound := r.orbs[orb.ObjectID]; isFound {
		return errors.New("duplicate campaign orb")
	}
	r.orbs[orb.ObjectID] = orb
	return nil
}

func (r *OrbRegistry) Orb(objectID uint32) (Orb, bool) {
	if r == nil || objectID == 0 {
		return Orb{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	orb, isFound := r.orbs[objectID]
	return orb, isFound
}

func (r *OrbRegistry) Orbs() []Orb {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	orbs := make([]Orb, 0, len(r.orbs))
	for _, orb := range r.orbs {
		orbs = append(orbs, orb)
	}
	sort.Slice(orbs, func(left int, right int) bool {
		return orbs[left].ObjectID < orbs[right].ObjectID
	})
	return orbs
}

func (r *OrbRegistry) Contacts(
	start game.Vec3,
	end game.Vec3,
	maximumDistance float32,
	now time.Time,
) []Orb {
	if r == nil || !zonegeometry.IsFinite(start) ||
		!zonegeometry.IsFinite(end) ||
		!zonegeometry.IsFinitePositiveScalar(maximumDistance) {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	contacts := make([]Orb, 0, 1)
	for _, orb := range r.orbs {
		if !orb.AvailableAt.IsZero() && now.Before(orb.AvailableAt) {
			continue
		}
		position := game.Vec3{
			X: orb.Request.Destination.X,
			Y: orb.Request.Destination.Y,
			Z: orb.Request.Destination.Z,
		}
		if zonegeometry.SegmentIntersectsSphere(
			start, end, position, maximumDistance,
		) {
			contacts = append(contacts, orb)
		}
	}
	sort.Slice(contacts, func(left int, right int) bool {
		return contacts[left].ObjectID < contacts[right].ObjectID
	})
	return contacts
}

func (r *OrbRegistry) Remove(objectID uint32) bool {
	if r == nil || objectID == 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, isFound := r.orbs[objectID]
	delete(r.orbs, objectID)
	return isFound
}

func (r *OrbRegistry) Count() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.orbs)
}
