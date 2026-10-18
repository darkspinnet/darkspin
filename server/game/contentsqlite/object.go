package contentsqlite

import (
	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
)

func campaignInteractable(definition *contentsqlite.InteractableDefinition) *game.CampaignInteractableDefinition {
	if definition == nil {
		return nil
	}
	return &game.CampaignInteractableDefinition{
		UseLimit: definition.UseLimit, AbilityHash: definition.AbilityHash, AbilityName: definition.AbilityName,
		StartEventHash: definition.StartEventHash, StartEventName: definition.StartEventName,
		EndEventHash: definition.EndEventHash, EndEventName: definition.EndEventName,
		OptionalEventHash: definition.OptionalEventHash, OptionalEventName: definition.OptionalEventName,
		Challenge: definition.Challenge,
	}
}

func campaignCombatant(definition *contentsqlite.CombatantDefinition) *game.CampaignCombatantDefinition {
	if definition == nil {
		return nil
	}
	return &game.CampaignCombatantDefinition{
		DeathEventHash: definition.DeathEventHash, DeathEventName: definition.DeathEventName,
	}
}
