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

type PickupKind uint8

const (
	PickupUnknown PickupKind = iota
	PickupEquipment
	PickupCrystal
	PickupDNA
	PickupOrb
)

type PickupAdmission uint8

const (
	PickupRejectedNotFound PickupAdmission = iota
	PickupRejectedActor
	PickupRejectedReserved
	PickupRejectedRange
	PickupAccepted
)

type Pickup struct {
	Flight                sim.DropFlight
	ObjectID              uint32
	Kind                  PickupKind
	Position              game.Vec3
	SourcePosition        game.Vec3
	IsSourcePositionKnown bool
	ExpiresAt             time.Duration
}

type PickupCommand struct {
	ActorObjectID   uint32
	ActiveObjectID  uint32
	TargetObjectID  uint32
	ActorPosition   game.Vec3
	MaximumDistance float32
	SimulationTime  time.Duration
}

type PickupContactCommand struct {
	ActorObjectID   uint32
	ActiveObjectID  uint32
	TargetObjectID  uint32
	SegmentStart    game.Vec3
	SegmentEnd      game.Vec3
	MaximumDistance float32
	SimulationTime  time.Duration
	SampleTime      time.Time
}

type PickupRegistry struct {
	mu      sync.Mutex
	pickups map[uint32]Pickup
	set     *PickupSet
}

func NewPickupRegistry() *PickupRegistry {
	return &PickupRegistry{
		pickups: make(map[uint32]Pickup), set: NewPickupSet(),
	}
}

func (r *PickupRegistry) Register(pickup Pickup) error {
	if r == nil || pickup.ObjectID == 0 || pickup.Kind == PickupUnknown ||
		!zonegeometry.IsFinite(pickup.Position) || pickup.ExpiresAt < 0 ||
		(pickup.IsSourcePositionKnown &&
			!zonegeometry.IsFinite(pickup.SourcePosition)) {
		return errors.New("invalid pickup")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.set.Register(pickup.ObjectID) {
		return errors.New("duplicate pickup")
	}
	r.pickups[pickup.ObjectID] = pickup
	return nil
}

func (r *PickupRegistry) Pickup(objectID uint32) (Pickup, bool) {
	if r == nil || objectID == 0 {
		return Pickup{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	pickup, isFound := r.pickups[objectID]
	if isFound {
		pickup = pickup.at(time.Now())
		r.pickups[objectID] = pickup
	}
	return pickup, isFound
}

func (r *PickupRegistry) Snapshots() []Pickup {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	pickup := make([]Pickup, 0, len(r.pickups))
	for objectID, current := range r.pickups {
		if !r.set.IsAvailable(objectID) {
			continue
		}
		current = current.at(time.Now())
		r.pickups[objectID] = current
		pickup = append(pickup, current)
	}
	sort.Slice(pickup, func(left int, right int) bool {
		return pickup[left].ObjectID < pickup[right].ObjectID
	})
	return pickup
}

// SnapshotsAt omits pickups whose original simulation deadline has passed.
func (r *PickupRegistry) SnapshotsAt(now time.Duration) []Pickup {
	pickups := r.Snapshots()
	livePickups := make([]Pickup, 0, len(pickups))
	for _, pickup := range pickups {
		if pickup.ExpiresAt > 0 && now >= pickup.ExpiresAt {
			continue
		}
		livePickups = append(livePickups, pickup)
	}
	return livePickups
}

// Expire retires due pickups once, including entries reserved for collection.
func (r *PickupRegistry) Expire(now time.Duration) []Pickup {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	expiredPickups := make([]Pickup, 0)
	for objectID, pickup := range r.pickups {
		if pickup.ExpiresAt == 0 || now < pickup.ExpiresAt {
			continue
		}
		expiredPickups = append(expiredPickups, pickup)
		delete(r.pickups, objectID)
		r.set.Remove(objectID)
	}
	sort.Slice(expiredPickups, func(left int, right int) bool {
		return expiredPickups[left].ObjectID < expiredPickups[right].ObjectID
	})
	return expiredPickups
}

func (r *PickupRegistry) IsAvailable(objectID uint32) bool {
	if r == nil || objectID == 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.set.IsAvailable(objectID)
}

func (r *PickupRegistry) IsReserved(objectID uint32) bool {
	if r == nil || objectID == 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.set.IsReserved(objectID)
}

func (r *PickupRegistry) Reserve(command PickupCommand) (Pickup, PickupAdmission) {
	if r == nil || command.ActorObjectID == 0 ||
		command.ActorObjectID != command.ActiveObjectID ||
		!zonegeometry.IsFinite(command.ActorPosition) ||
		!zonegeometry.IsFinitePositiveScalar(command.MaximumDistance) {
		return Pickup{}, PickupRejectedActor
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	pickup, isFound := r.pickups[command.TargetObjectID]
	if !isFound {
		return Pickup{}, PickupRejectedNotFound
	}
	if pickup.ExpiresAt > 0 && command.SimulationTime >= pickup.ExpiresAt {
		return Pickup{}, PickupRejectedNotFound
	}
	pickup = pickup.at(time.Now())
	r.pickups[command.TargetObjectID] = pickup
	if r.set.IsReserved(command.TargetObjectID) {
		return pickup, PickupRejectedReserved
	}
	isInRange := zonegeometry.ContainsSphere(
		command.ActorPosition, pickup.Position, command.MaximumDistance,
	)
	if pickup.IsSourcePositionKnown && (pickup.Flight.StartedAt.IsZero() || pickup.Flight.IsProjectilePresent) {
		isInRange = isInRange || zonegeometry.ContainsSphere(
			command.ActorPosition, pickup.SourcePosition,
			command.MaximumDistance,
		)
	}
	if !isInRange {
		return pickup, PickupRejectedRange
	}
	if !r.set.Reserve(command.TargetObjectID) {
		return pickup, PickupRejectedNotFound
	}
	return pickup, PickupAccepted
}

func (r *PickupRegistry) ReserveContact(
	command PickupContactCommand,
) (Pickup, PickupAdmission) {
	if r == nil || command.ActorObjectID == 0 ||
		command.ActorObjectID != command.ActiveObjectID ||
		!zonegeometry.IsFinite(command.SegmentStart) ||
		!zonegeometry.IsFinite(command.SegmentEnd) ||
		!zonegeometry.IsFinitePositiveScalar(command.MaximumDistance) {
		return Pickup{}, PickupRejectedActor
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	pickup, isFound := r.pickups[command.TargetObjectID]
	if !isFound {
		return Pickup{}, PickupRejectedNotFound
	}
	if pickup.ExpiresAt > 0 && command.SimulationTime >= pickup.ExpiresAt {
		return Pickup{}, PickupRejectedNotFound
	}
	pickup = pickup.at(command.SampleTime)
	r.pickups[command.TargetObjectID] = pickup
	if r.set.IsReserved(command.TargetObjectID) {
		return pickup, PickupRejectedReserved
	}
	isInRange := zonegeometry.SegmentIntersectsSphere(
		command.SegmentStart, command.SegmentEnd, pickup.Position,
		command.MaximumDistance,
	)
	if pickup.IsSourcePositionKnown && (pickup.Flight.StartedAt.IsZero() || pickup.Flight.IsProjectilePresent) {
		isInRange = isInRange || zonegeometry.SegmentIntersectsSphere(
			command.SegmentStart, command.SegmentEnd,
			pickup.SourcePosition, command.MaximumDistance,
		)
	}
	if !isInRange {
		return pickup, PickupRejectedRange
	}
	if !r.set.Reserve(command.TargetObjectID) {
		return pickup, PickupRejectedNotFound
	}
	return pickup, PickupAccepted
}

func (r *PickupRegistry) Release(objectID uint32) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.set.Release(objectID)
}

func (r *PickupRegistry) Commit(objectID uint32) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.set.Commit(objectID) {
		return false
	}
	delete(r.pickups, objectID)
	r.set.Remove(objectID)
	return true
}

func (r *PickupRegistry) Remove(objectID uint32) bool {
	if r == nil || objectID == 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pickups, objectID)
	return r.set.Remove(objectID)
}

func (e Pickup) at(now time.Time) Pickup {
	if e.Flight.StartedAt.IsZero() || e.Flight.IsProjectilePresent {
		return e
	}
	e.Flight = e.Flight.At(now)
	e.Position = game.Vec3(e.Flight.Position)
	return e
}
