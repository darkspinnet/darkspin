package game

import "slices"

// SelectedBossPortalPositions returns authored portal anchors from the chosen
// layout. Missing anchors remain empty rather than using unselected placements.
func (e CampaignDirector) SelectedBossPortalPositions() []Vec3 {
	markerIDs := make([]uint32, 0)
	for markerID, definition := range e.selectedDefinitionsByMarkerID {
		if e.NounTypeForAsset(definition.NounName) == NounTypeBossPortal {
			markerIDs = append(markerIDs, markerID)
		}
	}
	slices.Sort(markerIDs)
	positions := make([]Vec3, 0, len(markerIDs))
	for _, markerID := range markerIDs {
		positions = append(positions, e.selectedDefinitionsByMarkerID[markerID].Position)
	}
	return positions
}
