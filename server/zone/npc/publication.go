package npc

import (
	"errors"
	"fmt"
	"math"

	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

// PublishInTargetRange exposes staged actors only when the server can acquire
// a live opposing target at the normal aggro boundary. Untargeted retail NPCs
// otherwise run client-side idle locomotion that the server cannot observe.
func (s *Session) PublishInTargetRange(targets []Target) ([]Snapshot, error) {
	if s == nil {
		return nil, errors.New("nil npc session")
	}
	for index, target := range targets {
		if target.ObjectID == 0 || !zonegeometry.IsFinite(target.Position) ||
			math.IsNaN(float64(target.FootprintRadius)) ||
			math.IsInf(float64(target.FootprintRadius), 0) ||
			target.FootprintRadius < 0 || target.Faction == FactionUnknown {
			return nil, fmt.Errorf("npcPublishTarget[%d]: invalid", index)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	published := make([]Snapshot, 0)
	for _, objectID := range s.objectIDs {
		npc := s.npcs[objectID]
		if npc.IsPublished || npc.IsDefeated || npc.Plan.IsFixture || npc.HitPoint <= 0 {
			continue
		}
		isTargetInRange := false
		for _, target := range targets {
			if !target.IsAlive || target.Faction == npc.Faction {
				continue
			}
			distance := s.aggroRadius + target.FootprintRadius +
				max(float32(0), npc.Plan.NPCProfile.FootprintRadius)
			if npc.Plan.Position.Sub(target.Position).Length() > distance {
				continue
			}
			isTargetInRange = true
			break
		}
		if !isTargetInRange {
			continue
		}
		npc.IsPublished = true
		s.npcs[objectID] = npc
		published = append(published, npc)
	}
	return published, nil
}

func (s *Session) SetPublished(
	objectIDs []uint32, isPublished bool,
) ([]Snapshot, error) {
	if s == nil {
		return nil, errors.New("nil npc session")
	}
	if len(objectIDs) == 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	seenObjectID := make(map[uint32]bool, len(objectIDs))
	for index, objectID := range objectIDs {
		npc, isFound := s.npcs[objectID]
		if objectID == 0 || !isFound || npc.IsDefeated {
			return nil, fmt.Errorf("npcPublicationMissing[%d]: %d", index, objectID)
		}
		if seenObjectID[objectID] {
			return nil, fmt.Errorf("npcPublicationDuplicate[%d]: %d", index, objectID)
		}
		if npc.IsPublished == isPublished {
			return nil, fmt.Errorf("npcPublicationUnchanged[%d]: %d", index, objectID)
		}
		seenObjectID[objectID] = true
	}
	changed := make([]Snapshot, 0, len(objectIDs))
	for _, objectID := range objectIDs {
		npc := s.npcs[objectID]
		npc.IsPublished = isPublished
		if !isPublished {
			npc.TargetObjectID = 0
			npc.TargetFaction = FactionUnknown
			npc.TargetOwner = ActionOwner{}
			npc.IsActionStarted = false
			npc.ActionOwner = ActionOwner{}
		}
		s.npcs[objectID] = npc
		changed = append(changed, npc)
	}
	return changed, nil
}
