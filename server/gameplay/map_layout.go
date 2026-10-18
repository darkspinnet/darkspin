package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e campaignPreparation) selectInitialMapLayout(
	director game.CampaignDirector, binding game.GameplayBinding,
) (game.CampaignDirector, error) {
	// Use the same adapter inputs as the prepare packet, including its native
	// condition mask and seed sentinel handling. Never roll from the game ID.
	message, err := zonePrepareMessage(binding)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("layoutPrepare: %w", err)
	}
	ordinals, err := director.SelectMapLayout(message.VariantSeed, message.ConditionMask)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("layoutSelect: %w", err)
	}
	selectedDirector, err := director.InitialMapLayout(message.VariantSeed, ordinals)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("layoutProject: %w", err)
	}
	if e.logger != nil {
		setsByOrdinal := make(map[int]game.CampaignDirectorMarkerSet, len(director.MarkerSets))
		for _, set := range director.MarkerSets {
			setsByOrdinal[set.Ordinal] = set
		}
		selectedAssets := make([]string, 0, len(ordinals))
		for _, ordinal := range ordinals {
			set := setsByOrdinal[ordinal]
			selectedAssets = append(selectedAssets, fmt.Sprintf("set=%d %s", ordinal,
				mapLayoutAssetLabel(set.Name, set.CatalogAsset)))
		}
		e.logger.Printf(
			"RakNet map layout game=%d level=%q stage=%d seed=%#x conditions=%#x level_catalog=%s selected_sets=%v",
			binding.GameID, director.Level, message.LevelIndex,
			message.VariantSeed, message.ConditionMask,
			mapLayoutAssetLabel(director.Level, director.LevelCatalogAsset), selectedAssets,
		)
	}
	return selectedDirector, nil
}

// auditSelectedFixtureTakeover ties each selected client object deletion to
// exactly one server-owned fixture before either packet is published.
func (e campaignPreparation) auditSelectedFixtureTakeover(
	director game.CampaignDirector, markers []game.CampaignDirectorMarker,
	deletedObjectIDs []uint32, plans []zonenpc.SpawnPlan,
) error {
	if len(markers) != len(deletedObjectIDs) || len(markers) != len(plans) {
		return fmt.Errorf("fixtureMapping: markers=%d deletions=%d plans=%d",
			len(markers), len(deletedObjectIDs), len(plans))
	}
	ordinalsBySet := make(map[string]int, len(director.MarkerSets))
	for _, set := range director.MarkerSets {
		ordinalsBySet[set.Name] = set.Ordinal
	}
	for index, marker := range markers {
		plan := plans[index]
		setOrdinal, isSelected := ordinalsBySet[marker.MarkerSetName]
		if !isSelected || deletedObjectIDs[index] != marker.MarkerID ||
			plan.LocusID != marker.MarkerID || plan.MarkerSetName != marker.MarkerSetName ||
			plan.NounName != marker.NounName || plan.Position != marker.Position ||
			plan.Rotation != marker.Rotation || plan.PlacementScale != marker.Scale || !plan.IsFixture {
			return fmt.Errorf("fixtureMapping[%d]: selected marker %d mismatch", index, marker.MarkerID)
		}
		if e.logger != nil {
			e.logger.Printf("RakNet fixture takeover level=%q set=%d marker=%d runtime=%d noun=%q position=(%.6f,%.6f,%.6f) rotation=(%.6f,%.6f,%.6f) scale=%.6f order=delete-before-create",
				director.Level, setOrdinal, marker.MarkerID, plan.ObjectID, plan.NounName,
				plan.Position.X, plan.Position.Y, plan.Position.Z,
				plan.Rotation.X, plan.Rotation.Y, plan.Rotation.Z, plan.PlacementScale)
		}
	}
	return nil
}

func mapLayoutAssetLabel(reference string, asset game.CampaignAssetIdentity) string {
	if asset.Ordinal == nil {
		return fmt.Sprintf("%q catalog=unresolved", reference)
	}
	return fmt.Sprintf("%q catalog=%d asset=%q source=%q", reference,
		*asset.Ordinal, asset.AssetName, asset.SourceName)
}
