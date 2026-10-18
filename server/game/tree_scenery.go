package game

import (
	"errors"
	"slices"
	"strings"
)

// ServerTreeScenery restores static tree nouns which the native map loader
// deliberately leaves to the server. Build 103 sub_9D3720 (127 sub_9D8720) returns server-only
// mask 0x200 for static noun type 0x6FEDAE4D when clientOnly (byte 4) is false;
// sub_9DAC70 consequently does not instantiate them on the client.
func (e CampaignDirector) ServerTreeScenery() ([]CampaignDirectorMarker, error) {
	if !e.IsInitialLayoutSelected {
		return nil, errors.New("tree scenery layout unavailable")
	}
	markerIDs := make([]uint32, 0)
	for markerID, definition := range e.selectedDefinitionsByMarkerID {
		if isServerTreeNoun(definition.NounName) {
			markerIDs = append(markerIDs, markerID)
		}
	}
	slices.Sort(markerIDs)
	markers := make([]CampaignDirectorMarker, 0, len(markerIDs))
	for _, markerID := range markerIDs {
		definition := e.selectedDefinitionsByMarkerID[markerID]
		markers = append(markers, CampaignDirectorMarker{
			MarkerID: markerID, NounName: definition.NounName,
			Position: definition.Position, Rotation: definition.Rotation,
			Scale: definition.Scale, IsVisible: definition.IsVisible,
			IsCollisionEnabled: definition.IsCollisionEnabled,
		})
	}
	return markers, nil
}

func isServerTreeNoun(nounName string) bool {
	// Audited packaged nouns: clientOnly=0, isFixed=1,
	// hasNetworkComponent=1, hasCombatantComponent=0. Other PHYS_ trees
	// include client-owned nouns and must not be republished by prefix alone.
	switch strings.ToLower(nounName) {
	case "phys_nocturna2_lvl2_herotree_yellow_1.noun",
		"phys_nocturna2_herotree_red_1.noun",
		"phys_tota_cypress_tree_2.noun",
		"phys_tota_tree_large_b.noun":
		return true
	default:
		return false
	}
}
