package gameplay

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

func (r campaignNPCActionRuntime) triggerArenaPlasmaSentinelActive(
	peerSession *gameplayPeerSession, timestamp uint64,
) ([][]byte, error) {
	run := peerSession.plasmaSentinelActive
	if run == nil || !run.isShieldActive || run.triggerCount >= plasmaSentinelMaximumPetCount ||
		peerSession.deployedObjectID != run.ownerObjectID ||
		peerSession.generation != run.generation || peerSession.binding.GameID != run.arenaGameID {
		return nil, nil
	}
	match := r.registry.arenaMatches[run.arenaGameID]
	if match == nil || !match.isCombatActive || match.abilityWorld != run.arenaWorld {
		return nil, nil
	}
	petIndex := int(run.triggerCount)
	rockPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: util.HashID("plasmasentinel_got_hit_rocks.ServerEventDef"), ObjectID: run.ownerObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("arenaPlasmaRocks: %w", err)
	}
	var removePacket []byte
	if petIndex == plasmaSentinelMaximumPetCount-1 {
		removePacket, err = raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: run.effectSlot + 1, IsRemovalRequested: true, IsHardStop: true,
			ObjectID: run.ownerObjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("arenaPlasmaShieldRemove: %w", err)
		}
	}
	petPackets, err := r.spawnArenaPlasmaSentinelPet(peerSession, run, match, timestamp, petIndex)
	if err != nil {
		return nil, fmt.Errorf("arenaPlasmaSpawn: %w", err)
	}
	run.triggerCount++
	packets := append([][]byte{rockPacket}, petPackets...)
	if run.triggerCount < plasmaSentinelMaximumPetCount {
		return packets, nil
	}
	run.isShieldActive = false
	if run.cancel != nil {
		run.cancel()
		run.cancel = nil
	}
	run.runtime.effectPool.Release(run.ownerObjectID, run.effectSlot)
	return append(packets, removePacket), nil
}

func (r campaignNPCActionRuntime) spawnArenaPlasmaSentinelPet(
	peerSession *gameplayPeerSession, run *plasmaSentinelActiveRun, match *arenaMatchState,
	timestamp uint64, petIndex int,
) ([][]byte, error) {
	if petIndex < 0 || petIndex >= len(run.pets) || run.pets[petIndex].objectID != 0 {
		return nil, fmt.Errorf("arenaPlasmaIndex: %d", petIndex)
	}
	if r.timer == nil {
		return nil, errors.New("Arena Plasma timer unavailable")
	}
	world := run.arenaWorld
	objectID, err := world.objectIDSession.Reserve(1)
	if err != nil {
		return nil, fmt.Errorf("arenaPlasmaID: %w", err)
	}
	instanceID, err := r.modifierPool.Allocate()
	if err != nil {
		return nil, fmt.Errorf("arenaPlasmaModifierID: %w", err)
	}
	hitPoint := r.program.NonPlayerHitPoint[util.HashID("PlasmaSentinelPet")]
	if hitPoint <= 0 {
		hitPoint = plasmaSentinelFallbackHitPoint
	}
	hitPoint = peerSession.petHitPoint(hitPoint)
	footprint, footprintErr := r.program.FootprintRadius("PlasmaSentinelPet.Noun")
	if footprintErr != nil || footprint <= 0 {
		footprint = plasmaSentinelFallbackFootprint
	}
	facing, err := geometryraknet.Forward(run.orientation)
	if err != nil {
		r.releaseArenaPlasmaModifier(instanceID)
		return nil, fmt.Errorf("arenaPlasmaFacing: %w", err)
	}
	angle := math.Atan2(float64(facing.Y), float64(facing.X)) - math.Pi/3
	if petIndex%2 == 1 {
		angle += 2 * math.Pi / 3
	}
	desired := game.Vec3(peerSession.playerPosition).Add(game.Vec3{
		X: float32(math.Cos(angle)) * 5, Y: float32(math.Sin(angle)) * 5,
	})
	position := desired
	if match.navigation != nil {
		projected, isProjected, projectionErr := zonenavigation.DirectMovementDestination(
			match.navigation, game.Vec3(peerSession.playerPosition), desired, footprint)
		if projectionErr != nil {
			r.releaseArenaPlasmaModifier(instanceID)
			return nil, fmt.Errorf("arenaPlasmaProjection: %w", projectionErr)
		}
		if isProjected {
			position = projected
		}
	}
	modifierPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: run.ownerObjectID, TargetObjectID: run.ownerObjectID,
		ModifierID: 0x95dcd059, InstanceID: instanceID,
		Duration: plasmaSentinelPetDuration, Timestamp: timestamp,
	})
	if err != nil {
		r.releaseArenaPlasmaModifier(instanceID)
		return nil, fmt.Errorf("arenaPlasmaModifier: %w", err)
	}
	modifierDelete, err := effectraknet.ModifierDelete(run.ownerObjectID, instanceID)
	if err != nil {
		r.releaseArenaPlasmaModifier(instanceID)
		return nil, fmt.Errorf("arenaPlasmaModifierDelete: %w", err)
	}
	createPackets, err := companionraknet.Create(raknet.ObjectCreateMessage{
		ObjectID: objectID, Noun: util.HashID("PlasmaSentinelPet.Noun"),
		PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
		Scale: 1, Team: uint8(peerSession.binding.Team), OwnerID: run.ownerObjectID,
		IsCollisionEnabled: true,
	})
	if err != nil {
		r.releaseArenaPlasmaModifier(instanceID)
		return nil, fmt.Errorf("arenaPlasmaCreate: %w", err)
	}
	attributePacket, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: objectID, Value: map[uint8]float32{
			11: zonecompanion.CompatibilityMovementSpeed,
			12: zonecompanion.CompatibilityMovementSpeed, 48: 0,
		},
	})
	if err != nil {
		r.releaseArenaPlasmaModifier(instanceID)
		return nil, fmt.Errorf("arenaPlasmaAttribute: %w", err)
	}
	spawnPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: util.HashID("PlasmaSentinelPetSpawn"), ObjectID: objectID,
		Position: raknet.Vector3{X: position.X, Y: position.Y, Z: position.Z},
	})
	if err != nil {
		r.releaseArenaPlasmaModifier(instanceID)
		return nil, fmt.Errorf("arenaPlasmaSpawnPacket: %w", err)
	}
	deletePacket, err := companionraknet.Defeat(objectID)
	if err != nil {
		r.releaseArenaPlasmaModifier(instanceID)
		return nil, fmt.Errorf("arenaPlasmaDefeatPacket: %w", err)
	}
	err = world.companion.Put(zonecompanion.Actor{
		UserID: peerSession.binding.UserID, PeerGeneration: peerSession.generation,
		ObjectID: objectID, OwnerObjectID: run.ownerObjectID,
		Noun: util.HashID("PlasmaSentinelPet.Noun"), Position: position,
		FootprintRadius: footprint, HitPoint: hitPoint, MaximumHitPoint: hitPoint,
		IsTargetable: true, IsCombatant: true,
	})
	if err != nil {
		r.releaseArenaPlasmaModifier(instanceID)
		return nil, fmt.Errorf("arenaPlasmaPut: %w", err)
	}
	petDamage := float32(0)
	if peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) {
		petDamage = peerSession.binding.Creatures[peerSession.deployedCreatureIndex].PetDamage
	}
	pet := &arenaCombatPetRun{
		runtime: run.runtime, packet: run.packet, world: world,
		sessionKey: run.sessionKey, gameID: run.arenaGameID,
		generation: run.generation, ownerObjectID: run.ownerObjectID,
		objectID: objectID, kind: arenaCombatPetPlasma, startedAt: run.startedAt,
		attackAbility: r.program.PlasmaSentinelPetBasic, petDamage: petDamage,
		deletePacket: deletePacket, modifierPacket: modifierDelete,
		modifierInstanceID: instanceID, plasmaOwner: run, plasmaIndex: petIndex,
		isPublished: true,
	}
	if world.combatPets == nil {
		world.combatPets = make(map[uint32]*arenaCombatPetRun)
	}
	world.combatPets[objectID] = pet
	run.pets[petIndex] = plasmaSentinelPetRun{objectID: objectID, modifierInstanceID: instanceID}
	expiry := plasmaSentinelPetExpiryStep{run: run, objectID: objectID}
	cancelExpiry, err := r.timer.Schedule(plasmaSentinelPetDuration, expiry.execute)
	if err == nil && cancelExpiry == nil {
		err = errors.New("Arena Plasma expiry cancellation unavailable")
	}
	if err != nil {
		pet.isPublished = false
		pet.retireLocked()
		return nil, fmt.Errorf("arenaPlasmaExpiry: %w", err)
	}
	pet.lifetimeCancel = cancelExpiry
	producer := raknet.ScheduledPacketProducer{Delay: arenaPetTargetPoll, Produce: pet.startMelee}
	// This reactive spawn runs inside the registry write lock. The producer
	// guard takes a read lock, so let the pet's own match/generation fence
	// validate the callback instead of deadlocking the hit continuation.
	cancelPoll, err := run.packet.Autonomous().ScheduleProducers([]raknet.ScheduledPacketProducer{producer})
	if err == nil && cancelPoll == nil {
		err = errors.New("Arena Plasma attack cancellation unavailable")
	}
	if err != nil {
		pet.isPublished = false
		pet.retireLocked()
		return nil, fmt.Errorf("arenaPlasmaAttack: %w", err)
	}
	pet.cycleCancel = cancelPoll
	packets := append([][]byte{modifierPacket}, createPackets...)
	return append(packets, attributePacket, spawnPacket), nil
}

func (r campaignNPCActionRuntime) releaseArenaPlasmaModifier(instanceID uint32) {
	err := r.modifierPool.Release(instanceID)
	if err != nil && r.logger != nil {
		r.logger.Printf("Arena Plasma Sentinel modifier release failed instance=%d: %v", instanceID, err)
	}
}
