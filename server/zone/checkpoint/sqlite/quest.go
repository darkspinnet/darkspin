package sqlite

import (
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
	zoneobjective "github.com/darkspinnet/darkspin/server/zone/objective"
	zonequest "github.com/darkspinnet/darkspin/server/zone/quest"
)

type questPositionPayload struct{ X, Y, Z float32 }
type questDropPayload struct {
	ID                    uint64
	ObjectiveID           uint32
	NounID                uint32
	SourceObjectID        uint32
	SourcePosition        questPositionPayload
	SourceFootprintRadius float32
}
type questCrystalPayload struct {
	Request       questDropPayload
	ObjectID      uint32
	Destination   questPositionPayload
	FlightElapsed time.Duration
	IsCollected   bool
	IsPublished   bool
}
type questEventPayload struct {
	Kind                       string
	ObjectID                   uint32
	KillPercent                float64
	IsInteractableUseAvailable bool
	TargetObjectID             uint32
	SourceObjectID             uint32
	Damage                     float64
	Healing                    float64
	DamageInteger              int32
	TargetTeam                 uint8
	SourceTeam                 uint8
	PlayerIndex                uint8
	IsTargetNPC                bool
	IsTargetDestructible       bool
	IsTargetPlayerControlled   bool
	IsSourcePlayerControlled   bool
	TargetMarkerID             uint32
	TargetAssetID              uint32
	ModifierDescriptor         uint32
	TargetPosition             questPositionPayload
	TargetFootprintRadius      float32
}

func encodeQuestDrop(req zoneobjective.DropRequest) questDropPayload {
	return questDropPayload{ID: req.ID, ObjectiveID: req.ObjectiveID, NounID: req.NounID,
		SourceObjectID: req.SourceObjectID, SourcePosition: questPositionPayload(req.SourcePosition),
		SourceFootprintRadius: req.SourceFootprintRadius}
}
func decodeQuestDrop(req questDropPayload) zoneobjective.DropRequest {
	return zoneobjective.DropRequest{ID: req.ID, ObjectiveID: req.ObjectiveID, NounID: req.NounID,
		SourceObjectID: req.SourceObjectID, SourcePosition: sim.Position(req.SourcePosition),
		SourceFootprintRadius: req.SourceFootprintRadius}
}

func encodeQuestState(encoded *checkpointPayload) {
	for _, req := range encoded.Snapshot.ObjectiveDrops {
		encoded.ObjectiveDrops = append(encoded.ObjectiveDrops, encodeQuestDrop(req))
	}
	for _, crystal := range encoded.Snapshot.QuestCrystals {
		encoded.QuestCrystals = append(encoded.QuestCrystals, questCrystalPayload{
			Request: encodeQuestDrop(crystal.Request), ObjectID: crystal.ObjectID,
			Destination: questPositionPayload(crystal.Destination), FlightElapsed: crystal.FlightElapsed,
			IsCollected: crystal.IsCollected, IsPublished: crystal.IsPublished})
	}
	for _, event := range encoded.Snapshot.ObjectiveEvents {
		encoded.ObjectiveEvents = append(encoded.ObjectiveEvents, questEventPayload{
			Kind: string(event.Kind), ObjectID: event.ObjectID, KillPercent: event.KillPercent,
			IsInteractableUseAvailable: event.IsInteractableUseAvailable,
			TargetObjectID:             event.TargetObjectID, SourceObjectID: event.SourceObjectID,
			Damage: event.Damage, Healing: event.Healing, DamageInteger: event.DamageInteger,
			TargetTeam: event.TargetTeam, SourceTeam: event.SourceTeam, PlayerIndex: event.PlayerIndex,
			IsTargetNPC: event.IsTargetNPC, IsTargetDestructible: event.IsTargetDestructible,
			IsTargetPlayerControlled: event.IsTargetPlayerControlled, IsSourcePlayerControlled: event.IsSourcePlayerControlled,
			TargetMarkerID: event.TargetMarkerID, TargetAssetID: event.TargetAssetID,
			ModifierDescriptor: event.ModifierDescriptor, TargetPosition: questPositionPayload(event.TargetPosition),
			TargetFootprintRadius: event.TargetFootprintRadius})
	}
}

func decodeQuestState(encoded checkpointPayload) ([]zoneobjective.DropRequest, []zonequest.State, []sim.LuaObjectiveEvent) {
	drops := make([]zoneobjective.DropRequest, 0, len(encoded.ObjectiveDrops))
	crystals := make([]zonequest.State, 0, len(encoded.QuestCrystals))
	events := make([]sim.LuaObjectiveEvent, 0, len(encoded.ObjectiveEvents))
	for _, req := range encoded.ObjectiveDrops {
		drops = append(drops, decodeQuestDrop(req))
	}
	for _, crystal := range encoded.QuestCrystals {
		crystals = append(crystals, zonequest.State{Request: decodeQuestDrop(crystal.Request), ObjectID: crystal.ObjectID,
			Destination: sim.Position(crystal.Destination), FlightElapsed: crystal.FlightElapsed,
			IsCollected: crystal.IsCollected, IsPublished: crystal.IsPublished})
	}
	for _, event := range encoded.ObjectiveEvents {
		events = append(events, sim.LuaObjectiveEvent{
			Kind: sim.LuaObjectiveEventKind(event.Kind), ObjectID: event.ObjectID, KillPercent: event.KillPercent,
			IsInteractableUseAvailable: event.IsInteractableUseAvailable,
			TargetObjectID:             event.TargetObjectID, SourceObjectID: event.SourceObjectID,
			Damage: event.Damage, Healing: event.Healing, DamageInteger: event.DamageInteger,
			TargetTeam: event.TargetTeam, SourceTeam: event.SourceTeam, PlayerIndex: event.PlayerIndex,
			IsTargetNPC: event.IsTargetNPC, IsTargetDestructible: event.IsTargetDestructible,
			IsTargetPlayerControlled: event.IsTargetPlayerControlled, IsSourcePlayerControlled: event.IsSourcePlayerControlled,
			TargetMarkerID: event.TargetMarkerID, TargetAssetID: event.TargetAssetID,
			ModifierDescriptor: event.ModifierDescriptor, TargetPosition: sim.Position(event.TargetPosition),
			TargetFootprintRadius: event.TargetFootprintRadius})
	}
	return drops, crystals, events
}
