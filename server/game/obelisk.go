package game

import (
	"fmt"
	"math"
)

// obeliskObjects discovers component-owned abilities independently of level
// event listeners. Native 9DDA50 chooses the marker definition before the noun
// definition; only a zero challenge inherits from the noun afterward.
func (e CampaignDirector) obeliskObjects() ([]CampaignScriptObject, error) {
	objects := make([]CampaignScriptObject, 0)
	markersByID := make(map[uint32]struct{})
	for _, set := range e.MarkerSets {
		for _, marker := range set.Markers {
			definition := marker.Interactable
			if definition == nil {
				definition = marker.NounInteractable
			}
			if definition == nil {
				continue
			}
			markersByID[marker.MarkerID] = struct{}{}
			if definition.AbilityName == nil || !isObeliskAbility(*definition.AbilityName) {
				continue
			}
			interactable, err := resolveCampaignInteractable(marker.Interactable, marker.NounInteractable)
			if err != nil {
				return nil, fmt.Errorf("obeliskDefinition[%d]: %w", marker.MarkerID, err)
			}
			if marker.MarkerID == 0 || marker.Name == "" || marker.NounName == "" ||
				!isFiniteCampaignPosition(marker.Position) || !isFiniteCampaignPosition(marker.Rotation) ||
				marker.Scale <= 0 || math.IsNaN(float64(marker.Scale)) || math.IsInf(float64(marker.Scale), 0) ||
				interactable.UseLimit == 0 || interactable.UseLimit < -1 {
				return nil, fmt.Errorf("obeliskMarker[%d]: invalid", marker.MarkerID)
			}
			objects = append(objects, CampaignScriptObject{
				Interactable:     interactable,
				MarkerSetOrdinal: set.Ordinal, MarkerSetName: set.Name, MarkerSetWeight: set.Weight,
				MarkerOrdinal: marker.Ordinal, MarkerID: marker.MarkerID, MarkerName: marker.Name,
				NounName: marker.NounName, Position: marker.Position, Rotation: marker.Rotation,
				Scale: marker.Scale, IsVisible: marker.IsVisible, IsCollisionEnabled: marker.IsCollisionEnabled,
				InteractableAbility: *interactable.AbilityName, InteractableUseLimit: interactable.UseLimit,
				InteractableChallenge: interactable.Challenge,
			})
		}
	}
	// Retain legacy projections for callers that have no structural component
	// placements. A component's explicit empty/other ability must not fall back
	// to an obsolete guessed listener callback.
	scripts, err := e.ScriptObjects()
	if err != nil {
		return nil, fmt.Errorf("obeliskScripts: %w", err)
	}
	for _, script := range scripts {
		_, isComponentOwned := markersByID[script.MarkerID]
		if isComponentOwned || !isObeliskAbility(script.InteractableAbility) {
			continue
		}
		objects = append(objects, script)
	}
	return objects, nil
}

func isObeliskAbility(name string) bool {
	return name == "InteractWithObelisk" || name == "InteractHealthObelisk"
}

func (e *CampaignScriptRegistry) isComponentRegistration(registration CampaignScriptRegistration) bool {
	object, isFound := e.interactablesByMarkerID[registration.MarkerID]
	return isFound && object.Interactable != nil &&
		object.InteractableAbility == registration.CallbackName &&
		object.InteractableUseLimit == registration.UseLimit &&
		object.InteractableChallenge == registration.Challenge
}
