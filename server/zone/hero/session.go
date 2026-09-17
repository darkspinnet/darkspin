package hero

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/darkspinnet/darkspin/server/game"
)

type Actor struct {
	UserID           uint64
	PeerGeneration   uint64
	ObjectID         uint32
	CreatureIndex    uint32
	Position         game.Vec3
	LinearVelocity   game.Vec3
	FootprintRadius  float32
	HitPoint         float32
	ManaPoint        float32
	MaximumHitPoint  float32
	MaximumManaPoint float32
	IsStealthed      bool
}

type Session struct {
	mu     sync.RWMutex
	actors map[uint64]Actor
}

func NewSession() *Session {
	return &Session{actors: make(map[uint64]Actor)}
}

func (s *Session) Put(actor Actor) error {
	if s == nil {
		return errors.New("nil hero session")
	}
	if actor.UserID == 0 || actor.PeerGeneration == 0 || actor.ObjectID == 0 ||
		!isFiniteActorPosition(actor.Position) ||
		!isFiniteActorPosition(actor.LinearVelocity) ||
		actor.FootprintRadius <= 0 ||
		math.IsNaN(float64(actor.FootprintRadius)) ||
		math.IsInf(float64(actor.FootprintRadius), 0) ||
		!isFiniteActorResource(actor.HitPoint, actor.MaximumHitPoint) ||
		!isFiniteActorResource(actor.ManaPoint, actor.MaximumManaPoint) {
		return errors.New("hero actor invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, isFound := s.actors[actor.UserID]
	if isFound && actor.PeerGeneration < current.PeerGeneration {
		return fmt.Errorf(
			"hero actor generation stale: got %d, want >= %d",
			actor.PeerGeneration, current.PeerGeneration,
		)
	}
	s.actors[actor.UserID] = actor
	return nil
}

func (s *Session) Snapshot(userID uint64, peerGeneration uint64) (Actor, bool) {
	if s == nil || userID == 0 || peerGeneration == 0 {
		return Actor{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	actor, isFound := s.actors[userID]
	return actor, isFound && actor.PeerGeneration == peerGeneration
}

func (s *Session) SnapshotByObjectID(objectID uint32) (Actor, bool) {
	if s == nil || objectID == 0 {
		return Actor{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, actor := range s.actors {
		if actor.ObjectID == objectID {
			return actor, true
		}
	}
	return Actor{}, false
}

func (s *Session) Snapshots() []Actor {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	actor := make([]Actor, 0, len(s.actors))
	for _, current := range s.actors {
		actor = append(actor, current)
	}
	sort.Slice(actor, func(left int, right int) bool {
		return actor[left].ObjectID < actor[right].ObjectID
	})
	return actor
}

func (s *Session) UpdatePose(
	userID uint64,
	peerGeneration uint64,
	objectID uint32,
	position game.Vec3,
	linearVelocity game.Vec3,
	footprintRadius float32,
) error {
	if s == nil {
		return errors.New("nil hero session")
	}
	if userID == 0 || peerGeneration == 0 || objectID == 0 ||
		!isFiniteActorPosition(position) ||
		!isFiniteActorPosition(linearVelocity) ||
		footprintRadius <= 0 ||
		math.IsNaN(float64(footprintRadius)) ||
		math.IsInf(float64(footprintRadius), 0) {
		return errors.New("hero pose invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	actor, isFound := s.actors[userID]
	if !isFound || actor.PeerGeneration != peerGeneration ||
		actor.ObjectID != objectID {
		return errors.New("hero actor unavailable")
	}
	actor.Position = position
	actor.LinearVelocity = linearVelocity
	actor.FootprintRadius = footprintRadius
	s.actors[userID] = actor
	return nil
}

func (s *Session) SetResources(
	userID uint64,
	peerGeneration uint64,
	objectID uint32,
	hitPoint float32,
	manaPoint float32,
) (Actor, error) {
	if s == nil {
		return Actor{}, errors.New("nil hero session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	actor, isFound := s.actors[userID]
	if !isFound || actor.PeerGeneration != peerGeneration ||
		actor.ObjectID != objectID {
		return Actor{}, errors.New("hero actor unavailable")
	}
	if !isFiniteActorResource(hitPoint, actor.MaximumHitPoint) ||
		!isFiniteActorResource(manaPoint, actor.MaximumManaPoint) {
		return Actor{}, errors.New("hero resource invalid")
	}
	if actor.HitPoint <= 0 && hitPoint > 0 {
		return Actor{}, errors.New("hero resurrection requires lifecycle operation")
	}
	previous := actor
	actor.HitPoint = hitPoint
	actor.ManaPoint = manaPoint
	s.actors[userID] = actor
	return previous, nil
}

func (s *Session) SetStealthed(
	userID uint64,
	peerGeneration uint64,
	objectID uint32,
	isStealthed bool,
) error {
	if s == nil {
		return errors.New("nil hero session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	actor, isFound := s.actors[userID]
	if !isFound || actor.PeerGeneration != peerGeneration ||
		actor.ObjectID != objectID {
		return errors.New("hero actor unavailable")
	}
	actor.IsStealthed = isStealthed
	s.actors[userID] = actor
	return nil
}

func (s *Session) Remove(userID uint64, peerGeneration uint64) bool {
	if s == nil || userID == 0 || peerGeneration == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	actor, isFound := s.actors[userID]
	if !isFound || actor.PeerGeneration != peerGeneration {
		return false
	}
	delete(s.actors, userID)
	return true
}

func isFiniteActorPosition(position game.Vec3) bool {
	return isFiniteActorScalar(position.X) &&
		isFiniteActorScalar(position.Y) &&
		isFiniteActorScalar(position.Z)
}

func isFiniteActorScalar(scalar float32) bool {
	return !math.IsNaN(float64(scalar)) && !math.IsInf(float64(scalar), 0)
}

func isFiniteActorResource(current float32, maximum float32) bool {
	return isFiniteActorScalar(current) && isFiniteActorScalar(maximum) &&
		current >= 0 && maximum > 0 && current <= maximum
}
