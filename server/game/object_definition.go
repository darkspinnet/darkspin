package game

import "errors"

type CampaignInteractableDefinition struct {
	UseLimit          int32
	AbilityHash       uint32
	AbilityName       *string
	StartEventHash    uint32
	StartEventName    *string
	EndEventHash      uint32
	EndEventName      *string
	OptionalEventHash uint32
	OptionalEventName *string
	Challenge         int32
}

type CampaignCombatantDefinition struct {
	DeathEventHash uint32
	DeathEventName *string
}

// resolveCampaignInteractable selects the entire marker definition when
// present, otherwise the noun definition. Only a zero challenge inherits;
// empty ability and event slots stay empty, and raw source data stays intact.
func resolveCampaignInteractable(
	marker *CampaignInteractableDefinition, noun *CampaignInteractableDefinition,
) (*CampaignInteractableDefinition, error) {
	selected := marker
	if selected == nil {
		selected = noun
	}
	if selected == nil {
		return nil, nil
	}
	resolved := *selected
	if resolved.Challenge != 0 {
		return &resolved, nil
	}
	if noun == nil {
		return nil, errors.New("interactable noun challenge unavailable")
	}
	resolved.Challenge = noun.Challenge
	return &resolved, nil
}
