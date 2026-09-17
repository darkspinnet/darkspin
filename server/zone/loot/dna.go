package loot

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

type DNAPickup struct {
	ObjectID    uint32
	Amount      uint32
	Position    game.Vec3
	AvailableAt time.Time
}

type DNASession struct {
	mu                sync.RWMutex
	pickups           map[uint32]DNAPickup
	reservedObjectIDs map[uint32]bool
}

type DNAGranter interface {
	GrantDNA(context.Context, int64, uint32) (uint32, error)
}

type DNAReservation struct {
	session *DNASession
	pickup  DNAPickup
	isLive  bool
}

func NewDNASession() *DNASession {
	return &DNASession{
		pickups:           make(map[uint32]DNAPickup),
		reservedObjectIDs: make(map[uint32]bool),
	}
}

func (s *DNASession) Add(pickup DNAPickup) error {
	if s == nil || pickup.ObjectID == 0 || pickup.Amount == 0 ||
		!isFiniteDNA(positionComponents(pickup.Position)) {
		return errors.New("invalid DNA pickup")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, isFound := s.pickups[pickup.ObjectID]; isFound {
		return errors.New("duplicate DNA pickup")
	}
	s.pickups[pickup.ObjectID] = pickup
	return nil
}

func (s *DNASession) Remove(objectID uint32) bool {
	if s == nil || objectID == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reservedObjectIDs[objectID] {
		return false
	}
	if _, isFound := s.pickups[objectID]; !isFound {
		return false
	}
	delete(s.pickups, objectID)
	return true
}

func (s *DNASession) Lookup(objectID uint32) (DNAPickup, bool) {
	if s == nil || objectID == 0 {
		return DNAPickup{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	pickup, isFound := s.pickups[objectID]
	return pickup, isFound
}

func (s *DNASession) Snapshots() []DNAPickup {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	pickup := make([]DNAPickup, 0, len(s.pickups))
	for objectID, current := range s.pickups {
		if s.reservedObjectIDs[objectID] {
			continue
		}
		pickup = append(pickup, current)
	}
	sort.Slice(pickup, func(left int, right int) bool {
		return pickup[left].ObjectID < pickup[right].ObjectID
	})
	return pickup
}

func (s *DNASession) ReserveContact(
	start game.Vec3, end game.Vec3, now time.Time, radius float32,
) (*DNAReservation, bool) {
	if s == nil || radius <= 0 ||
		!isFiniteDNA(positionComponents(start)) ||
		!isFiniteDNA(positionComponents(end)) {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	objectID := make([]uint32, 0, len(s.pickups))
	for id, pickup := range s.pickups {
		if s.reservedObjectIDs[id] || now.Before(pickup.AvailableAt) ||
			dnaSegmentDistance(start, end, pickup.Position) > radius {
			continue
		}
		objectID = append(objectID, id)
	}
	if len(objectID) == 0 {
		return nil, false
	}
	sort.Slice(objectID, func(left int, right int) bool {
		return objectID[left] < objectID[right]
	})
	pickup := s.pickups[objectID[0]]
	s.reservedObjectIDs[pickup.ObjectID] = true
	return &DNAReservation{
		session: s, pickup: pickup, isLive: true,
	}, true
}

func (r *DNAReservation) Pickup() DNAPickup {
	if r == nil {
		return DNAPickup{}
	}
	return r.pickup
}

func (r *DNAReservation) Commit() bool {
	if r == nil || r.session == nil || !r.isLive {
		return false
	}
	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	if !r.session.reservedObjectIDs[r.pickup.ObjectID] {
		return false
	}
	delete(r.session.reservedObjectIDs, r.pickup.ObjectID)
	delete(r.session.pickups, r.pickup.ObjectID)
	r.isLive = false
	return true
}

func (r *DNAReservation) Grant(
	ctx context.Context, granter DNAGranter, userID int64, currentDNA uint32,
) (uint32, error) {
	if r == nil || r.session == nil || !r.isLive {
		return 0, errors.New("invalid DNA reservation")
	}
	if granter == nil {
		return 0, errors.New("nil DNA granter")
	}
	if r.pickup.Amount > math.MaxUint32-currentDNA {
		return 0, errors.New("DNA overflow")
	}
	dna, err := granter.GrantDNA(ctx, userID, r.pickup.Amount)
	if err != nil {
		return 0, fmt.Errorf("grant: %w", err)
	}
	if !r.Commit() {
		return 0, errors.New("DNA reservation commit missing")
	}
	return dna, nil
}

func (r *DNAReservation) Release() bool {
	if r == nil || r.session == nil || !r.isLive {
		return false
	}
	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	if !r.session.reservedObjectIDs[r.pickup.ObjectID] {
		return false
	}
	delete(r.session.reservedObjectIDs, r.pickup.ObjectID)
	r.isLive = false
	return true
}

func (s *DNASession) Count() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.pickups)
}

func positionComponents(position game.Vec3) [3]float32 {
	return [3]float32{position.X, position.Y, position.Z}
}

func isFiniteDNA(component [3]float32) bool {
	for _, coordinate := range component {
		if math.IsNaN(float64(coordinate)) ||
			math.IsInf(float64(coordinate), 0) {
			return false
		}
	}
	return true
}

func dnaSegmentDistance(start game.Vec3, end game.Vec3, point game.Vec3) float32 {
	segmentX := end.X - start.X
	segmentY := end.Y - start.Y
	segmentZ := end.Z - start.Z
	lengthSquared := segmentX*segmentX + segmentY*segmentY + segmentZ*segmentZ
	if lengthSquared == 0 {
		return dnaDistance(start, point)
	}
	projection := ((point.X-start.X)*segmentX +
		(point.Y-start.Y)*segmentY +
		(point.Z-start.Z)*segmentZ) / lengthSquared
	projection = max(float32(0), min(float32(1), projection))
	closest := game.Vec3{
		X: start.X + segmentX*projection,
		Y: start.Y + segmentY*projection,
		Z: start.Z + segmentZ*projection,
	}
	return dnaDistance(closest, point)
}

func dnaDistance(first game.Vec3, second game.Vec3) float32 {
	deltaX := first.X - second.X
	deltaY := first.Y - second.Y
	deltaZ := first.Z - second.Z
	return float32(math.Sqrt(float64(
		deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ,
	)))
}
