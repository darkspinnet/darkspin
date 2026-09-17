package npc

import "errors"

// RetireMarkerSet removes every remaining actor in one authored marker set
// from live simulation without replacing the session that owns other sets.
func (s *Session) RetireMarkerSet(
	markerSetName string,
) ([]Snapshot, error) {
	if s == nil {
		return nil, errors.New("nil npc session")
	}
	if markerSetName == "" {
		return nil, errors.New("empty npc marker set")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	retired := make([]Snapshot, 0)
	for _, objectID := range s.objectIDs {
		npc := s.npcs[objectID]
		if npc.IsDefeated || npc.Plan.IsFixture ||
			npc.Plan.MarkerSetName != markerSetName {
			continue
		}
		npc.HitPoint = 0
		npc.IsDefeated = true
		npc.TargetObjectID = 0
		npc.TargetFaction = FactionUnknown
		npc.TargetOwner = ActionOwner{}
		npc.IsActionStarted = false
		npc.ActionOwner = ActionOwner{}
		s.npcs[objectID] = npc
		retired = append(retired, npc)
	}
	return retired, nil
}
