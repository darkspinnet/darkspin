package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

const fieldMedicDroneSpawnRadius = float32(1)

func (e *gameplayPeerSession) spawnFieldMedicDrone(
	program Programs, objectID uint32,
) ([][]byte, error) {
	if e == nil || e.zone == nil || e.zone.Companion() == nil || objectID == 0 ||
		e.deployedObjectID == 0 || e.deployedCreatureIndex >= uint32(len(e.binding.Creatures)) ||
		e.binding.Creatures[e.deployedCreatureIndex].PassiveAbility != util.HashID("FieldMedicPassive") {
		return nil, errors.New("field medic drone owner unavailable")
	}
	stalePackets, err := e.stopFieldMedicDrone()
	if err != nil {
		return nil, fmt.Errorf("fieldMedicDroneStale: %w", err)
	}
	count := 1
	if e.isOverdriveActiveAt(time.Now()) {
		count = 3
	}
	packets := stalePackets
	for index := 0; index < count; index++ {
		if index > 0 {
			objectID, err = e.reserveCampaignProjectileIDs(1, 2000)
			if err != nil {
				return nil, e.rollbackFieldMedicDroneSpawn(fmt.Errorf("fieldMedicExtraID: %w", err))
			}
		}
		actorPackets, spawnErr := e.spawnFieldMedicDroneActor(program, objectID, index, count)
		if spawnErr != nil {
			return nil, e.rollbackFieldMedicDroneSpawn(fmt.Errorf("fieldMedicDroneActor: %w", spawnErr))
		}
		packets = append(packets, actorPackets...)
	}
	return packets, nil
}

func (e *gameplayPeerSession) rollbackFieldMedicDroneSpawn(spawnErr error) error {
	packets, cleanupErr := e.stopFieldMedicDrone()
	if cleanupErr != nil {
		return fmt.Errorf("fieldMedicSpawnCleanup: %w", errors.Join(spawnErr, cleanupErr))
	}
	if len(packets) > 0 {
		// Actor creation packets have not been returned to the transport yet.
		// Cleanup removes their server-owned companions without publication.
	}
	return fmt.Errorf("fieldMedicSpawn: %w", spawnErr)
}

func (e *gameplayPeerSession) syncFieldMedicDrones(program Programs, now time.Time) ([][]byte, error) {
	if e == nil || e.deployedCreatureIndex >= uint32(len(e.binding.Creatures)) ||
		e.binding.Creatures[e.deployedCreatureIndex].PassiveAbility != util.HashID("FieldMedicPassive") ||
		e.deployedHitPoint() <= 0 {
		return nil, nil
	}
	if len(e.fieldMedicDroneObjectIDs) > 0 {
		return nil, nil
	}
	objectID, err := e.reserveCampaignProjectileIDs(1, 2000)
	if err != nil {
		return nil, fmt.Errorf("fieldMedicSyncID: %w", err)
	}
	packets, err := e.spawnFieldMedicDrone(program, objectID)
	if err != nil {
		return nil, fmt.Errorf("fieldMedicSyncSpawn: %w", err)
	}
	return packets, nil
}

func (e *gameplayPeerSession) spawnFieldMedicDroneActor(
	program Programs, objectID uint32, index, count int,
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
	angle := 2 * math.Pi * float64(index) / float64(count)
	desiredPosition := ownerPosition.Add(game.Vec3{
		X: fieldMedicDroneSpawnRadius * float32(math.Cos(angle)),
		Y: fieldMedicDroneSpawnRadius * float32(math.Sin(angle)),
	})
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
	createPackets, err := companionraknet.Create(raknet.ObjectCreateMessage{
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
	e.fieldMedicDroneObjectIDs = append(e.fieldMedicDroneObjectIDs, objectID)
	packets := createPackets
	packets = append(packets, attributePacket)
	return packets, nil
}

func (e *gameplayPeerSession) stopFieldMedicDrone() ([][]byte, error) {
	if e == nil {
		return nil, nil
	}
	for _, attack := range e.fieldMedicDroneAttacks {
		attack.Stop()
	}
	e.fieldMedicDroneAttacks = nil
	objectIDs := append([]uint32(nil), e.fieldMedicDroneObjectIDs...)
	e.fieldMedicDroneObjectIDs = nil
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
