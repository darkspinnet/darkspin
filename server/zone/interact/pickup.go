package interact

import (
	"errors"
	"sort"
	"sync"

	"github.com/darkspinnet/darkspin/server/game"
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
	ObjectID              uint32
	Kind                  PickupKind
	Position              game.Vec3
	SourcePosition        game.Vec3
	IsSourcePositionKnown bool
}

type PickupCommand struct {
	ActorObjectID   uint32
	ActiveObjectID  uint32
	TargetObjectID  uint32
	ActorPosition   game.Vec3
	MaximumDistance float32
}

type PickupContactCommand struct {
	ActorObjectID   uint32
	ActiveObjectID  uint32
	TargetObjectID  uint32
	SegmentStart    game.Vec3
	SegmentEnd      game.Vec3
	MaximumDistance float32
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
		!zonegeometry.IsFinite(pickup.Position) ||
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
		pickup = append(pickup, current)
	}
	sort.Slice(pickup, func(left int, right int) bool {
		return pickup[left].ObjectID < pickup[right].ObjectID
	})
	return pickup
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
	if r.set.IsReserved(command.TargetObjectID) {
		return pickup, PickupRejectedReserved
	}
	isInRange := zonegeometry.ContainsSphere(
		command.ActorPosition, pickup.Position, command.MaximumDistance,
	)
	if pickup.IsSourcePositionKnown {
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
	if r.set.IsReserved(command.TargetObjectID) {
		return pickup, PickupRejectedReserved
	}
	isInRange := zonegeometry.SegmentIntersectsSphere(
		command.SegmentStart, command.SegmentEnd, pickup.Position,
		command.MaximumDistance,
	)
	if pickup.IsSourcePositionKnown {
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
