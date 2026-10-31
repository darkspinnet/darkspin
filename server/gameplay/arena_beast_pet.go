package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

func (e *gameplayPeerSession) stopArenaBeastPet() ([][]byte, error) {
	run := e.beastPetResurrection
	world := run.arenaWorld
	objectID := e.beastPetObjectID
	e.beastPetResurrection = nil
	e.beastPetObjectID = 0
	e.beastPetDamage = 0
	e.beastPetDamageIncrease = 0
	packets, err := run.stopArena(e)
	if err != nil {
		return nil, fmt.Errorf("arenaBeastStopResurrection: %w", err)
	}
	if e.beastPetEnrage != nil {
		enrage := e.beastPetEnrage
		e.beastPetEnrage = nil
		if enrage.cancel != nil {
			enrage.cancel()
			enrage.cancel = nil
		}
		world.companion.ClearBodyScale(objectID)
		world.companion.RemoveBuff(objectID,
			zonecompanion.Buff{DamageBuff: beastPetEnrageDamageIncrease})
		modifierPacket, modErr := effectraknet.ModifierDelete(objectID, enrage.instanceID)
		if modErr != nil {
			return nil, fmt.Errorf("arenaBeastStopEnrage: %w", modErr)
		}
		releaseErr := enrage.runtime.modifierPool.Release(enrage.instanceID)
		if releaseErr != nil && enrage.runtime.logger != nil {
			enrage.runtime.logger.Printf("Arena Beast enrage release: %v", releaseErr)
		}
		packets = append(packets, modifierPacket)
	}
	pet := world.combatPets[objectID]
	if pet == nil {
		return packets, nil
	}
	pet.isPublished = false
	pet.retireLocked()
	packets = append(packets, pet.deletePacket)
	return packets, nil
}

func (r campaignAbilityCommandRuntime) spawnArenaBeastPet(
	member *gameplayPeerSession, world *arenaAbilityWorld, packet raknet.Packet,
	objectID uint32, ownerObjectID uint32, position game.Vec3, petDamage float32,
) ([][]byte, error) {
	hitPoint := r.program.NonPlayerHitPoint[util.HashID("BeastSentinelPet")]
	if hitPoint <= 0 {
		hitPoint = beastPetFallbackHitPoint
	}
	hitPoint = member.petHitPoint(hitPoint)
	footprint, footprintErr := r.program.FootprintRadius("BeastSentinelPet.Noun")
	if footprintErr != nil || footprint <= 0 {
		footprint = beastPetFallbackFootprint
	}
	createPackets, err := companionraknet.Create(raknet.ObjectCreateMessage{
		ObjectID: objectID, Noun: util.HashID("BeastSentinelPet.Noun"),
		PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
		Scale: 1, Team: uint8(member.binding.Team), OwnerID: ownerObjectID,
		IsCollisionEnabled: true})
	if err != nil {
		return nil, fmt.Errorf("arenaBeastCreate: %w", err)
	}
	messages := []raknet.ApplicationMessage{
		raknet.AttributeDataUpdateMessage{ObjectID: objectID,
			Value: map[uint8]float32{11: 0, 12: 0, 48: 0}},
		raknet.ServerEventMessage{Asset: util.HashID("beastSentinel_summon_burrowIn.ServerEventDef"),
			ObjectID: objectID, Position: raknet.Vector3(position)},
		raknet.SetAnimationStateMessage{ObjectID: objectID,
			State: util.HashID("beastsentinel_pet_spawn"), Scale: 1},
	}
	for index, message := range messages {
		encoded, marshalErr := raknet.MarshalApplication(message)
		if marshalErr != nil {
			return nil, fmt.Errorf("arenaBeastMarshal[%d]: %w", index, marshalErr)
		}
		createPackets = append(createPackets, encoded)
	}
	deletePacket, err := companionraknet.Defeat(objectID)
	if err != nil {
		return nil, fmt.Errorf("arenaBeastDelete: %w", err)
	}
	err = world.companion.Put(zonecompanion.Actor{
		UserID: member.binding.UserID, PeerGeneration: member.generation,
		ObjectID: objectID, OwnerObjectID: ownerObjectID,
		Noun: util.HashID("BeastSentinelPet.Noun"), Position: position,
		FootprintRadius: footprint, HitPoint: hitPoint, MaximumHitPoint: hitPoint,
		AggroRadius:  r.program.NonPlayerAggroRanges[util.HashID("BeastSentinelPet.Noun")],
		IsTargetable: true, IsCombatant: true})
	if err != nil {
		return nil, fmt.Errorf("arenaBeastPut: %w", err)
	}
	pet := &arenaCombatPetRun{
		runtime: r, packet: packet, world: world, sessionKey: packet.Address.String(),
		gameID: member.binding.GameID, generation: member.generation,
		ownerObjectID: ownerObjectID, objectID: objectID, kind: arenaCombatPetBeast,
		startedAt: r.now(), attackAbility: r.program.BeastPetBasic,
		petDamage: petDamage, deletePacket: deletePacket, isPublished: true,
	}
	if world.combatPets == nil {
		world.combatPets = make(map[uint32]*arenaCombatPetRun)
	}
	world.combatPets[objectID] = pet
	member.beastPetObjectID = objectID
	member.beastPetDamage = petDamage
	return createPackets, nil
}
