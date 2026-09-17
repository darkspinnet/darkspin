package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneinteract "github.com/darkspinnet/darkspin/server/zone/interact"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
)

type tutorialCapsulePlan struct {
	objectID uint32
	nounName string
	position game.Vec3
	kind     sim.OrbDropKind
}

func planTutorialCapsules(
	firstObjectID uint32,
) ([]tutorialCapsulePlan, uint32, error) {
	definitions := []tutorialCapsulePlan{
		{nounName: "HealthOrbPlaced.Noun", position: game.Vec3{X: 72.98147, Y: -30.69774, Z: 20.41953}, kind: sim.HealthOrbDrop},
		{nounName: "HealthOrbPlaced.Noun", position: game.Vec3{X: 79.41098, Y: -20.56364, Z: 20.44288}, kind: sim.HealthOrbDrop},
		{nounName: "HealthOrbPlaced.Noun", position: game.Vec3{X: 86.08772, Y: -11.43577, Z: 20.44230}, kind: sim.HealthOrbDrop},
		{nounName: "ManaOrbPlaced.Noun", position: game.Vec3{X: 97.54021, Y: 6.21943, Z: 21.11426}, kind: sim.ManaOrbDrop},
		{nounName: "ManaOrbPlaced.Noun", position: game.Vec3{X: 107.44933, Y: 10.47627, Z: 24.28472}, kind: sim.ManaOrbDrop},
		{nounName: "ManaOrbPlaced.Noun", position: game.Vec3{X: 120.08572, Y: 13.83584, Z: 25.58805}, kind: sim.ManaOrbDrop},
		{nounName: "HealthOrbPlaced.Noun", position: game.Vec3{X: 297.67712, Y: 174.65744, Z: 25.59114}, kind: sim.HealthOrbDrop},
		{nounName: "ManaOrbPlaced.Noun", position: game.Vec3{X: 302.40802, Y: 188.08199, Z: 24.31705}, kind: sim.ManaOrbDrop},
		{nounName: "HealthOrbPlaced.Noun", position: game.Vec3{X: 304.25351, Y: 201.31111, Z: 22.66250}, kind: sim.HealthOrbDrop},
		{nounName: "ManaOrbPlaced.Noun", position: game.Vec3{X: 300.22745, Y: 215.78809, Z: 20.81947}, kind: sim.ManaOrbDrop},
	}
	if firstObjectID == 0 ||
		uint64(firstObjectID)+uint64(len(definitions)) > uint64(zoneobject.ProjectileIDStart) {
		return nil, firstObjectID, errors.New("tutorial capsule object ids unavailable")
	}
	for index := range definitions {
		definitions[index].objectID = firstObjectID + uint32(index)
	}
	return definitions, firstObjectID + uint32(len(definitions)), nil
}

func (s *gameplayPeerSession) unlockTutorialCapsules() ([][]byte, error) {
	if s == nil || s.binding.Mode != game.ModeTutorial ||
		s.isTutorialCapsuleDropUnlocked {
		return nil, nil
	}
	if s.zone == nil || s.zone.Orbs() == nil || s.zone.Pickups() == nil ||
		len(s.tutorialCapsulePlans) == 0 {
		return nil, errors.New("tutorial capsule state unavailable")
	}
	packets := make([][]byte, 0, len(s.tutorialCapsulePlans)*3)
	for index, plan := range s.tutorialCapsulePlans {
		encoded, err := marshalTutorialCapsule(plan)
		if err != nil {
			return nil, fmt.Errorf("tutorialCapsuleMarshal[%d]: %w", index, err)
		}
		packets = append(packets, encoded...)
	}
	registeredObjectIDs := make([]uint32, 0, len(s.tutorialCapsulePlans))
	for index, plan := range s.tutorialCapsulePlans {
		position := sim.Position{
			X: plan.position.X, Y: plan.position.Y, Z: plan.position.Z,
		}
		request := sim.OrbPickupRequest{
			Role: sim.Role(fmt.Sprintf("tutorialCapsule.%d", plan.objectID)),
			Kind: plan.kind, NounName: plan.nounName,
			Position: position, Destination: position,
		}
		err := s.zone.Orbs().Add(zoneinteract.Orb{
			ObjectID: plan.objectID, Request: request,
		})
		if err != nil {
			s.rollbackTutorialCapsules(registeredObjectIDs)
			return nil, fmt.Errorf("tutorialCapsuleTrack[%d]: %w", index, err)
		}
		err = s.registerCampaignPickup(
			zoneinteract.PickupOrb, plan.objectID, position, position,
		)
		if err != nil {
			s.zone.Orbs().Remove(plan.objectID)
			s.rollbackTutorialCapsules(registeredObjectIDs)
			return nil, fmt.Errorf("tutorialCapsuleRegister[%d]: %w", index, err)
		}
		registeredObjectIDs = append(registeredObjectIDs, plan.objectID)
	}
	s.isTutorialCapsuleDropUnlocked = true
	return packets, nil
}

func (s *gameplayPeerSession) rollbackTutorialCapsules(objectIDs []uint32) {
	if s == nil || s.zone == nil {
		return
	}
	for _, objectID := range objectIDs {
		s.zone.Orbs().Remove(objectID)
		s.zone.Pickups().Remove(objectID)
	}
}

func marshalTutorialCapsule(plan tutorialCapsulePlan) ([][]byte, error) {
	position := raknet.Vector3{
		X: plan.position.X, Y: plan.position.Y, Z: plan.position.Z,
	}
	messages := []raknet.ApplicationMessage{
		raknet.EnemyObjectCreateMessage{
			ObjectID: plan.objectID, Noun: util.HashID(plan.nounName),
			Position: position, Scale: 1, IsCollidable: true,
		},
		raknet.ObjectUpdateMessage{
			ObjectID: plan.objectID, PositionX: position.X,
			PositionY: position.Y, PositionZ: position.Z, IsVisible: true,
		},
		raknet.ObjectPlayerMoveMessage{
			ObjectID: plan.objectID, GoalFlags: 0x20, GoalPosition: position,
		},
	}
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("tutorialCapsuleState[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
