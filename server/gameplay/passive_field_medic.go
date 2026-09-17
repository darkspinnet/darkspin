package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

const fieldMedicDroneSpawnRadius = float32(1)

func (e *gameplayPeerSession) spawnFieldMedicDrone(
	program Programs, objectID uint32,
) ([][]byte, error) {
	if e == nil || e.zone == nil || e.zone.Companion() == nil ||
		objectID == 0 || e.deployedObjectID == 0 ||
		e.deployedCreatureIndex >= uint32(len(e.binding.Creatures)) {
		return nil, errors.New("field medic drone spawn unavailable")
	}
	creature := e.binding.Creatures[e.deployedCreatureIndex]
	if creature.PassiveAbility != util.HashID("FieldMedicPassive") {
		return nil, errors.New("field medic passive unavailable")
	}
	stalePackets, err := e.stopFieldMedicDrone()
	if err != nil {
		return nil, fmt.Errorf("fieldMedicDroneStale: %w", err)
	}
	hitPoint := program.NonPlayerHitPoint[util.HashID("SentryDrone")]
	if hitPoint <= 0 {
		hitPoint = 1
	}
	hitPoint = e.petHitPoint(hitPoint)
	footprintRadius, err := program.FootprintRadius("SentryDrone.Noun")
	if err != nil || footprintRadius <= 0 {
		footprintRadius = 0.5
	}
	ownerPosition := game.Vec3(e.playerPosition)
	desiredPosition := ownerPosition.Add(game.Vec3{X: fieldMedicDroneSpawnRadius})
	position, isProjected, err := zonenavigation.DirectMovementDestination(
		e.zone.Navigation(), ownerPosition, desiredPosition, footprintRadius,
	)
	if err != nil {
		return nil, fmt.Errorf("fieldMedicDronePosition: %w", err)
	}
	if !isProjected && e.zone.Navigation() == nil {
		position = desiredPosition
	}
	err = e.zone.Companion().Put(zonecompanion.Actor{
		UserID: e.binding.UserID, PeerGeneration: e.generation,
		ObjectID: objectID, OwnerObjectID: e.deployedObjectID,
		Noun:     util.HashID("SentryDrone.Noun"),
		Position: position, FootprintRadius: footprintRadius,
		HitPoint: hitPoint, MaximumHitPoint: hitPoint,
		IsTargetable: false, IsCombatant: true,
	})
	if err != nil {
		return nil, fmt.Errorf("fieldMedicDronePut: %w", err)
	}
	createPacket, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
		ObjectID: objectID, Noun: util.HashID("SentryDrone.Noun"),
		PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
		Scale: 1, Team: 1, OwnerID: e.deployedObjectID,
		IsCollisionEnabled: true,
	})
	if err != nil {
		e.zone.Companion().Remove(objectID)
		return nil, fmt.Errorf("fieldMedicDroneCreate: %w", err)
	}
	attributePacket, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: objectID,
		Value: map[uint8]float32{
			11: zonecompanion.CompatibilityMovementSpeed,
			12: zonecompanion.CompatibilityMovementSpeed,
			48: 0,
		},
	})
	if err != nil {
		e.zone.Companion().Remove(objectID)
		return nil, fmt.Errorf("fieldMedicDroneAttribute: %w", err)
	}
	e.fieldMedicDroneObjectID = objectID
	packets := append(stalePackets, createPacket, attributePacket)
	return packets, nil
}

func (e *gameplayPeerSession) stopFieldMedicDrone() ([][]byte, error) {
	if e == nil {
		return nil, nil
	}
	if e.fieldMedicDroneAttack != nil {
		e.fieldMedicDroneAttack.Stop()
		e.fieldMedicDroneAttack = nil
	}
	objectIDs := make([]uint32, 0, 1)
	if e.fieldMedicDroneObjectID != 0 {
		objectIDs = append(objectIDs, e.fieldMedicDroneObjectID)
	}
	e.fieldMedicDroneObjectID = 0
	if e.zone != nil && e.zone.Companion() != nil {
		for _, companion := range e.zone.Companion().Snapshots() {
			if companion.UserID != e.binding.UserID ||
				companion.Noun != util.HashID("SentryDrone.Noun") {
				continue
			}
			isTracked := false
			for _, objectID := range objectIDs {
				isTracked = isTracked || objectID == companion.ObjectID
			}
			if !isTracked {
				objectIDs = append(objectIDs, companion.ObjectID)
			}
		}
		for _, objectID := range objectIDs {
			e.zone.Companion().Remove(objectID)
		}
	}
	if len(objectIDs) == 0 {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
		ObjectID: objectIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("fieldMedicDroneDelete: %w", err)
	}
	return [][]byte{packet}, nil
}
