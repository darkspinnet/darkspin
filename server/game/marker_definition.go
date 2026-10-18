package game

import "github.com/darkspinnet/darkspin/server/util"

// CampaignMarkerDefinition is an authored placement, independent of visibility,
// director role and whether a live object is spawned from it.
type CampaignMarkerDefinition struct {
	Ordinal                 int
	MarkerID                uint32
	NounName                string
	Position                Vec3
	Rotation                Vec3
	Scale                   float32
	IsVisible               bool
	IsCollisionEnabled      bool
	TeleporterTriggerRadius float32
	Teleporter              *CampaignTeleporterDefinition
}

type CampaignTeleporterDefinition struct {
	DestinationMarkerID       uint32
	TriggerVolume             *CampaignTriggerVolumeDefinition
	IsTriggerCreationDeferred bool
}

func (e *CampaignTeleporterDefinition) isTraversalCallback() bool {
	if e == nil || e.TriggerVolume == nil {
		return false
	}
	callbackHash := e.TriggerVolume.OnEnterHash
	return callbackHash == util.HashID("Teleporter_OnEnter") ||
		callbackHash == util.HashID("TunnelTeleporter_OnEnter")
}

// SelectedMarkerDefinition implements the full uint32 selected-marker lookup.
// Absence must not fall back to an unselected definition or a live-object roster.
func (e CampaignDirector) SelectedMarkerDefinition(markerID uint32) (CampaignMarkerDefinition, bool) {
	definition, isFound := e.selectedDefinitionsByMarkerID[markerID]
	return definition, isFound
}

// TeleportDestination follows the source's generic TeleporterDef through the
// selected index. A missing source, definition or destination has no route.
func (e CampaignDirector) TeleportDestination(markerID uint32) (Vec3, bool) {
	source, isFound := e.SelectedMarkerDefinition(markerID)
	if !isFound || source.Teleporter == nil {
		return Vec3{}, false
	}
	destination, isFound := e.SelectedMarkerDefinition(source.Teleporter.DestinationMarkerID)
	return destination.Position, isFound
}
