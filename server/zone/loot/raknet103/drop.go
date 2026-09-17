package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	interactraknet "github.com/darkspinnet/darkspin/server/zone/interact/raknet103"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
)

const lobMovementType = uint8(4)

type OrbDrop struct {
	CreatePacket       []byte
	PresentationPacket []byte
	LocomotionPacket   []byte
}

type DNACollectionRequest struct {
	Slot           uint8
	DNA            uint32
	Amount         uint32
	ActorObjectID  uint32
	PickupObjectID uint32
	Position       raknet.Vector3
}

func MarshalDNACollection(req DNACollectionRequest) ([][]byte, error) {
	if req.Amount == 0 || req.ActorObjectID == 0 || req.PickupObjectID == 0 {
		return nil, errors.New("invalid DNA collection")
	}
	updatePacket, err := raknet.MarshalApplication(raknet.LabsPlayerDNAUpdateMessage{
		Slot: req.Slot, DNA: req.DNA,
	})
	if err != nil {
		return nil, fmt.Errorf("dnaUpdate: %w", err)
	}
	pickupPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset:    util.HashID("dna_pickup.ServerEventDef"),
		ObjectID: req.ActorObjectID, Position: req.Position,
		TextValue: req.Amount,
	})
	if err != nil {
		return nil, fmt.Errorf("dnaPickup: %w", err)
	}
	deletePacket, err := interactraknet.DeletePickup(req.PickupObjectID)
	if err != nil {
		return nil, fmt.Errorf("dnaDelete: %w", err)
	}
	return [][]byte{updatePacket, pickupPacket, deletePacket}, nil
}

func MarshalOrbDrop(request sim.OrbPickupRequest, pickupObjectID uint32) (OrbDrop, error) {
	if pickupObjectID == 0 || request.Role == "" ||
		(request.Kind != sim.HealthOrbDrop && request.Kind != sim.ManaOrbDrop &&
			request.Kind != sim.ResurrectionOrbDrop) {
		return OrbDrop{}, errors.New("invalid orb drop request")
	}
	wantNoun := "HealthOrb.Noun"
	wantEvent := "health_orb_drop.ServerEventDef"
	if request.Kind == sim.ManaOrbDrop {
		wantNoun = "manaorb.Noun"
		wantEvent = "mana_orb_drop.ServerEventDef"
	}
	if request.Kind == sim.ResurrectionOrbDrop {
		wantNoun = "ResurrectOrb.Noun"
		wantEvent = "resurrect_orb_drop.ServerEventDef"
	}
	if request.NounName != wantNoun || request.EventName != wantEvent {
		return OrbDrop{}, fmt.Errorf("orbAsset: %s/%s", request.NounName, request.EventName)
	}
	createPacket, err := raknet.MarshalApplication(raknet.EnemyObjectCreateMessage{
		ObjectID: pickupObjectID, Noun: util.HashID(request.NounName),
		Position: raknet.Vector3{
			X: request.Position.X, Y: request.Position.Y, Z: request.Position.Z,
		},
		Scale: 1, IsCollidable: true, MovementType: lobMovementType,
	})
	if err != nil {
		return OrbDrop{}, fmt.Errorf("createMarshal: %w", err)
	}
	presentationPacket, err := raknet.MarshalApplication(raknet.DropPresentationMessage{
		Asset: util.HashID(request.EventName),
		Position: raknet.Vector3{
			X: request.Position.X, Y: request.Position.Y, Z: request.Position.Z,
		},
	})
	if err != nil {
		return OrbDrop{}, fmt.Errorf("presentationMarshal: %w", err)
	}
	locomotion, err := interactraknet.CrystalLobLocomotion(sim.CrystalPickupRequest{
		Role: request.Role, Position: request.Position, Destination: request.Destination,
		Lob: request.Lob,
	}, pickupObjectID)
	if err != nil {
		return OrbDrop{}, fmt.Errorf("locomotionMessage: %w", err)
	}
	locomotionPacket, err := raknet.MarshalApplication(locomotion)
	if err != nil {
		return OrbDrop{}, fmt.Errorf("locomotionMarshal: %w", err)
	}
	return OrbDrop{
		CreatePacket: createPacket, PresentationPacket: presentationPacket,
		LocomotionPacket: locomotionPacket,
	}, nil
}

func MarshalCrystalDrop(
	request sim.CrystalPickupRequest, objectID uint32,
) ([][]byte, error) {
	if objectID == 0 || request.Role == "" || request.NounName == "" || request.CrystalLevel < 0 {
		return nil, errors.New("invalid campaign crystal drop")
	}
	createPacket, err := raknet.MarshalApplication(raknet.EnemyObjectCreateMessage{
		ObjectID: objectID, Noun: util.HashID(request.NounName),
		Position: raknet.Vector3{X: request.Position.X, Y: request.Position.Y, Z: request.Position.Z},
		Scale:    1, IsCollidable: true, MovementType: lobMovementType,
	})
	if err != nil {
		return nil, fmt.Errorf("crystalCreate: %w", err)
	}
	dataPacket, err := raknet.MarshalApplication(raknet.CrystalLootDataUpdateMessage{
		ObjectID: objectID, CrystalLevel: request.CrystalLevel,
	})
	if err != nil {
		return nil, fmt.Errorf("crystalData: %w", err)
	}
	locomotion, err := interactraknet.CrystalLobLocomotion(request, objectID)
	if err != nil {
		return nil, fmt.Errorf("crystalLob: %w", err)
	}
	locomotionPacket, err := raknet.MarshalApplication(locomotion)
	if err != nil {
		return nil, fmt.Errorf("crystalLobMarshal: %w", err)
	}
	return [][]byte{createPacket, dataPacket, locomotionPacket}, nil
}

func MarshalDNADrop(plan zoneloot.DNAPlan) ([][]byte, error) {
	if plan.ObjectID == 0 || plan.Amount == 0 {
		return nil, errors.New("invalid campaign DNA drop")
	}
	createPacket, err := raknet.MarshalApplication(raknet.EnemyObjectCreateMessage{
		ObjectID: plan.ObjectID, Noun: util.HashID("DNA.Noun"),
		Position: raknet.Vector3{X: plan.Source.X, Y: plan.Source.Y, Z: plan.Source.Z},
		Scale:    1, IsCollidable: true, MovementType: lobMovementType,
	})
	if err != nil {
		return nil, fmt.Errorf("dnaCreate: %w", err)
	}
	dataPacket, err := raknet.MarshalApplication(raknet.LootDataUpdateMessage{
		ObjectID: plan.ObjectID, DNAAmount: float32(plan.Amount),
	})
	if err != nil {
		return nil, fmt.Errorf("dnaData: %w", err)
	}
	locomotion, err := interactraknet.CrystalLobLocomotion(sim.CrystalPickupRequest{
		Role: "dna", Position: plan.Source, Destination: plan.Destination, Lob: plan.Lob,
	}, plan.ObjectID)
	if err != nil {
		return nil, fmt.Errorf("dnaLob: %w", err)
	}
	locomotionPacket, err := raknet.MarshalApplication(locomotion)
	if err != nil {
		return nil, fmt.Errorf("dnaLobMarshal: %w", err)
	}
	return [][]byte{createPacket, dataPacket, locomotionPacket}, nil
}

func MarshalEquipmentDrop(
	plan zoneloot.EquipmentPlan, part sporenet.Part,
) ([][]byte, error) {
	if plan.ObjectID == 0 || plan.NounName == "" || part.RigblockAssetID == 0 {
		return nil, errors.New("invalid campaign equipment drop")
	}
	createPacket, err := raknet.MarshalApplication(raknet.EnemyObjectCreateMessage{
		ObjectID: plan.ObjectID, Noun: util.HashID(plan.NounName),
		Position: raknet.Vector3{X: plan.Source.X, Y: plan.Source.Y, Z: plan.Source.Z},
		Scale:    1, IsCollidable: true, MovementType: lobMovementType,
	})
	if err != nil {
		return nil, fmt.Errorf("equipmentCreate: %w", err)
	}
	interactablePacket, err := raknet.MarshalApplication(raknet.InteractableDataUpdateMessage{
		ObjectID: plan.ObjectID, TimesUsed: 0, UsesAllowed: 1,
		Ability: util.HashID("PickUpLoot"),
	})
	if err != nil {
		return nil, fmt.Errorf("equipmentInteractable: %w", err)
	}
	dataPacket, err := raknet.MarshalApplication(raknet.LootDataUpdateMessage{
		ObjectID: plan.ObjectID, ItemID: uint64(plan.ObjectID),
		RigblockAsset: part.RigblockAssetHash,
		SuffixAsset:   part.SuffixAssetHash, PrefixAsset: part.PrefixAssetHash,
		SecondaryPrefixAsset: part.PrefixSecondaryAssetHash,
		ItemLevel:            int32(part.Level), Rarity: int32(part.Rarity),
	})
	if err != nil {
		return nil, fmt.Errorf("equipmentData: %w", err)
	}
	eventPacket, err := raknet.MarshalApplication(raknet.DropPresentationMessage{
		Asset:    util.HashID("loot_spawn.ServerEventDef"),
		Position: raknet.Vector3{X: plan.Source.X, Y: plan.Source.Y, Z: plan.Source.Z},
	})
	if err != nil {
		return nil, fmt.Errorf("equipmentEvent: %w", err)
	}
	locomotion, err := interactraknet.CrystalLobLocomotion(sim.CrystalPickupRequest{
		Role: "loot", NounName: plan.NounName,
		Position: plan.Source, Destination: plan.Destination, Lob: plan.Lob,
	}, plan.ObjectID)
	if err != nil {
		return nil, fmt.Errorf("equipmentLob: %w", err)
	}
	locomotionPacket, err := raknet.MarshalApplication(locomotion)
	if err != nil {
		return nil, fmt.Errorf("equipmentLobMarshal: %w", err)
	}
	return [][]byte{createPacket, interactablePacket, dataPacket, eventPacket, locomotionPacket}, nil
}

func MarshalEquipmentAward(
	part sporenet.Part, objectID uint32, position raknet.Vector3,
) ([]byte, error) {
	if part.ID == 0 || part.ReferenceID == 0 || part.RigblockAssetHash == 0 ||
		objectID == 0 || !isFinitePosition(position) {
		return nil, errors.New("invalid campaign equipment award")
	}
	packet, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset:    util.HashID("loot_acquired.ServerEventDef"),
		ObjectID: objectID, Position: position,
		ClientEventID: util.HashID("LootAwarded"),
		Loot: &raknet.ServerEventLoot{
			ReferenceID: part.ReferenceID, InstanceID: part.ID,
			RigblockID: part.RigblockAssetHash, SuffixAsset: part.SuffixAssetHash,
			PrefixAsset1: part.PrefixAssetHash, PrefixAsset2: part.PrefixSecondaryAssetHash,
			ItemLevel: int32(part.Level), Rarity: int32(part.Rarity),
			CreationTime: part.CreationDate,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("equipmentAwardMarshal: %w", err)
	}
	return packet, nil
}

func isFinitePosition(position raknet.Vector3) bool {
	return !math.IsNaN(float64(position.X)) && !math.IsInf(float64(position.X), 0) &&
		!math.IsNaN(float64(position.Y)) && !math.IsInf(float64(position.Y), 0) &&
		!math.IsNaN(float64(position.Z)) && !math.IsInf(float64(position.Z), 0)
}
