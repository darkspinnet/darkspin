package game

import (
	"errors"
	"slices"
	"strings"
)

// ServerScenery restores audited static nouns which the native map loader
// deliberately leaves to the server. Build 103 sub_9D3720 returns server-only
// mask 0x200 for static noun type 0x6FEDAE4D when clientOnly (byte 4) is false;
// sub_9D5D40 consequently does not instantiate them on the client.
func (e CampaignDirector) ServerScenery() ([]CampaignDirectorMarker, error) {
	if !e.IsInitialLayoutSelected {
		return nil, errors.New("server scenery layout unavailable")
	}
	markerIDs := make([]uint32, 0)
	for markerID, definition := range e.selectedDefinitionsByMarkerID {
		if isServerSceneryNoun(definition.NounName) {
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

func isServerSceneryNoun(nounName string) bool {
	// Audited packaged nouns: clientOnly=0, isFixed=1,
	// hasNetworkComponent=1, hasCombatantComponent=0. Build 103 9D1DF0
	// registers noun byte +216 with the network owner. The design box returns
	// mask 0x200, so the client needs its ordinary server ObjectCreate before
	// its collision can agree with the selected server navigation obstacle.
	// Other PHYS_ nouns include client-owned objects and must not be
	// republished by prefix alone.
	// The rocks and flowers below also returned 0x200 for every selected
	// placement in the build-103 Nocturna/Verdanth capture. Despite the
	// flower_dest name, that noun is fixed static scenery with no combatant
	// component or native client-owned placement override.
	switch strings.ToLower(nounName) {
	case "phys_nocturna2_lvl2_herotree_yellow_1.noun",
		"phys_design_object_block_3x3.noun",
		"phys_nocturna2_herotree_red_1.noun",
		"phys_moon1_rock_large_a.noun",
		"phys_nocturna2_rock_large_a.noun",
		"phys_tota_object_rock_pile_b.noun",
		"phys_tota_rock_grass_c.noun",
		"phys_tota_heroflower_a.noun",
		"tota_plant_flower_dest.noun",
		"phys_tota_cypress_tree_2.noun",
		"phys_tota_tree_large_b.noun":
		return true
	default:
		return false
	}
}
