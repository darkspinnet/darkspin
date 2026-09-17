package npc

import (
	"errors"
	"slices"

	"github.com/darkspinnet/darkspin/server/game"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type CandidateRequest struct {
	NounNames         []string
	TargetPosition    game.Vec3
	StopDistance      float32
	IsActionReadyOnly bool
}

type CandidateResult struct {
	Snapshots       []Snapshot
	IsMatchingAlive bool
}

func (s *Session) SelectCandidates(req CandidateRequest) (CandidateResult, error) {
	if s == nil {
		return CandidateResult{}, errors.New("nil npc session")
	}
	if len(req.NounNames) == 0 ||
		!zonegeometry.IsFinite(req.TargetPosition) ||
		!zonegeometry.IsFinitePositiveScalar(req.StopDistance) {
		return CandidateResult{}, errors.New("invalid npc candidate request")
	}
	nounName := make(map[string]bool, len(req.NounNames))
	for _, currentNounName := range req.NounNames {
		if currentNounName == "" {
			return CandidateResult{}, errors.New("empty npc candidate noun")
		}
		nounName[currentNounName] = true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	objectIDs := slices.Clone(s.objectIDs)
	slices.Sort(objectIDs)
	result := CandidateResult{
		Snapshots: make([]Snapshot, 0, len(objectIDs)),
	}
	for _, objectID := range objectIDs {
		npc, isFound := s.npcs[objectID]
		if !isFound || npc.IsDefeated || !npc.IsPublished || npc.HitPoint <= 0 ||
			!nounName[npc.Plan.NounName] {
			continue
		}
		result.IsMatchingAlive = true
		if req.IsActionReadyOnly && npc.IsActionStarted {
			continue
		}
		if !zonegeometry.ContainsSphereStrict(
			req.TargetPosition, npc.Plan.Position, req.StopDistance,
		) {
			continue
		}
		result.Snapshots = append(result.Snapshots, npc)
	}
	return result, nil
}
