package npc

import (
	"errors"

	"github.com/darkspinnet/darkspin/server/game"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type SphereRequest struct {
	Center game.Vec3
	Radius float32
}

func (s *Session) ActorsInSphere(req SphereRequest) ([]Snapshot, error) {
	if s == nil {
		return nil, errors.New("nil npc session")
	}
	if !zonegeometry.IsFinite(req.Center) ||
		!zonegeometry.IsFinitePositiveScalar(req.Radius) {
		return nil, errors.New("invalid npc sphere request")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshots := make([]Snapshot, 0)
	for _, objectID := range s.objectIDs {
		npc := s.npcs[objectID]
		if npc.IsDefeated || !npc.IsPublished || npc.Plan.IsFixture ||
			npc.HitPoint <= 0 {
			continue
		}
		if !zonegeometry.ContainsSphereStrict(
			req.Center, npc.Plan.Position, req.Radius,
		) {
			continue
		}
		snapshots = append(snapshots, npc)
	}
	return snapshots, nil
}
