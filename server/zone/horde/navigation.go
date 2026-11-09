package horde

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/util"
)

// ConfigureNavigation installs dormant authored colliders before actor
// navigation is configured. Admission and completion own their shared lifetime.
func (e *Session) ConfigureNavigation(director game.CampaignDirector, mesh *navigation.Mesh) (*navigation.Mesh, error) {
	if e == nil {
		return nil, errors.New("horde navigation unavailable")
	}
	if mesh == nil {
		return nil, nil
	}
	sets, err := director.HordeNavigationObstacles()
	if err != nil {
		return nil, fmt.Errorf("navigationBoxes: %w", err)
	}
	obstacles := make([]navigation.Obstacle, 0)
	markerIDsByMarkerSet := make(map[string][]uint32, len(sets))
	markerSetNames := make([]string, 0, len(sets))
	for markerSetName := range sets {
		markerSetNames = append(markerSetNames, markerSetName)
	}
	slices.Sort(markerSetNames)
	for _, markerSetName := range markerSetNames {
		boxes := sets[markerSetName]
		markerIDs := make([]uint32, 0, len(boxes))
		for _, box := range boxes {
			obstacles = append(obstacles, navigation.Obstacle{
				MarkerID: box.MarkerID, Position: navigation.Vec3(box.Position),
				Minimum: navigation.Vec3(box.Minimum), Maximum: navigation.Vec3(box.Maximum),
				Yaw: box.Yaw, IsInitiallyDisabled: true,
			})
			markerIDs = append(markerIDs, box.MarkerID)
		}
		markerIDsByMarkerSet[strings.ToLower(markerSetName)] = markerIDs
	}
	configured, err := mesh.WithAdditionalObstacles(obstacles)
	if err != nil {
		return nil, fmt.Errorf("navigationInstall: %w", err)
	}
	links := make([]navigation.TraversalLink, 0)
	linkMarkerIDsByMarkerSet := make(map[string][]uint32)
	for _, set := range director.MarkerSets {
		for _, definition := range set.Definitions {
			link, isFound := selectedGateLink(director, set.Name, definition)
			if !isFound {
				continue
			}
			volume := link.Route.TriggerVolume
			if volume.Shape != uint32(game.TriggerBox) && volume.Shape != uint32(game.TriggerSphere) {
				return nil, fmt.Errorf("navigationTrigger[%d]: unsupported shape", definition.MarkerID)
			}
			links = append(links, navigation.TraversalLink{
				MarkerID: definition.MarkerID, Source: navigation.Vec3(link.Route.Source),
				Destination: navigation.Vec3(link.Route.Destination), Rotation: navigation.Vec3(link.Route.Rotation),
				Offset: navigation.Vec3(volume.Offset), BoxDimensions: navigation.Vec3(volume.BoxDimensions),
				SphereRadius: volume.SphereRadius, IsSphere: volume.Shape == uint32(game.TriggerSphere),
			})
			key := strings.ToLower(set.Name)
			linkMarkerIDsByMarkerSet[key] = append(linkMarkerIDsByMarkerSet[key], definition.MarkerID)
		}
	}
	configured, err = configured.WithTraversalLinks(links)
	if err != nil {
		return nil, fmt.Errorf("navigationLinks: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.navigationMesh = configured
	e.barrierMarkerIDsByMarkerSet = markerIDsByMarkerSet
	e.linkMarkerIDsByMarkerSet = linkMarkerIDsByMarkerSet
	for markerSetName, encounter := range e.encountersByMarkerSet {
		e.setNavigationEnabled(markerSetName, encounter.isGateActive)
	}
	return configured, nil
}

func (e *Session) setNavigationEnabled(markerSetName string, isEnabled bool) {
	for _, markerID := range e.barrierMarkerIDsByMarkerSet[strings.ToLower(markerSetName)] {
		e.navigationMesh.SetObstacleEnabled(markerID, isEnabled)
	}
	for _, markerID := range e.linkMarkerIDsByMarkerSet[strings.ToLower(markerSetName)] {
		e.navigationMesh.SetTraversalLinkEnabled(markerID, isEnabled)
	}
}

type GateLink struct {
	MarkerSetName string
	Route         game.CampaignTeleportRoute
}

// GateLinks follows build-103 ActivateHordeGateTeleporter: a selected trigger
// placement creates one ordered link to its selected destination. The active
// encounter owns both this link and its associated fence collider lifetime.
func (e *Session) GateLinks(director game.CampaignDirector) []GateLink {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	links := make([]GateLink, 0)
	for _, set := range director.MarkerSets {
		encounter := e.encountersByMarkerSet[strings.ToLower(set.Name)]
		if encounter == nil || !encounter.isGateActive {
			continue
		}
		for _, definition := range set.Definitions {
			link, isFound := selectedGateLink(director, set.Name, definition)
			if !isFound {
				continue
			}
			links = append(links, link)
		}
	}
	return links
}

func selectedGateLink(director game.CampaignDirector, markerSetName string, definition game.CampaignMarkerDefinition) (GateLink, bool) {
	if !strings.EqualFold(definition.NounName, "HordeGateTeleporter.Noun") ||
		definition.Teleporter == nil || definition.Teleporter.TriggerVolume == nil ||
		definition.Teleporter.TriggerVolume.OnEnterHash != util.HashID("HordeGateTeleporter_OnEnter") {
		return GateLink{}, false
	}
	destination, isFound := director.TeleportDestination(definition.MarkerID)
	if !isFound {
		return GateLink{}, false
	}
	return GateLink{MarkerSetName: markerSetName, Route: game.CampaignTeleportRoute{
		MarkerID: definition.MarkerID, DestinationMarkerID: definition.Teleporter.DestinationMarkerID,
		Source: definition.Position, Destination: destination, Rotation: definition.Rotation,
		TriggerVolume: definition.Teleporter.TriggerVolume,
	}}, true
}
