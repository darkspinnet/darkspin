package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
)

type arenaSagePassive struct {
	run       *summonPassiveRun
	startedAt time.Time
}

func (e gameplayPendingRuntime) pollArenaSagePets(packet raknet.Packet) ([][]byte, error) {
	e.registry.mutex.Lock()
	owner, isFound := e.registry.sessions[packet.Address.String()]
	if !isFound || owner.binding.Mode != game.ModeArena || !owner.isArenaCombatActive {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	pets := make([]*arenaCombatPetRun, 0)
	packets := make([][]byte, 0)
	for key, member := range e.registry.sessions {
		if member.binding.GameID != owner.binding.GameID || !member.isArenaCombatActive ||
			(member.binding.UserID != owner.binding.UserID && member.binding.BotOwnerUserID != owner.binding.UserID) ||
			member.deployedHitPoint() <= 0 || member.deployedCreatureIndex >= uint32(len(member.binding.Creatures)) ||
			member.binding.Creatures[member.deployedCreatureIndex].PassiveAbility != util.HashID("SupportHealerPassiveModifier") {
			continue
		}
		memberPacket := packet
		if member.binding.IsServerBot {
			memberPacket = serverBotPacket(packet, member)
		}
		spawnPackets, spawnedPets, err := e.advanceArenaSageLocked(memberPacket, key, &member)
		e.registry.sessions[key] = member
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("sageAdvance: %w", err)
		}
		packets = append(packets, spawnPackets...)
		pets = append(pets, spawnedPets...)
	}
	if len(packets) > 0 {
		e.registry.queueArenaForcePulsePacketsLocked(owner.binding.GameID, packets)
	}
	e.registry.mutex.Unlock()
	for _, pet := range pets {
		err := pet.scheduleMeleePoll(sageSpawnDuration)
		if err != nil {
			pet.fail(err)
			return nil, fmt.Errorf("sageAttack: %w", err)
		}
	}
	// Arena publication queues each packet for every real participant.
	return nil, nil
}

func (e gameplayPendingRuntime) advanceArenaSageLocked(packet raknet.Packet, key string, member *gameplayPeerSession) ([][]byte, []*arenaCombatPetRun, error) {
	world, err := e.registry.arenaAbilityWorldLocked(member.binding.GameID)
	if err != nil {
		return nil, nil, fmt.Errorf("sageWorld: %w", err)
	}
	if world.sages == nil {
		world.sages = make(map[uint32]*arenaSagePassive)
	}
	now := e.now()
	passive := world.sages[member.deployedObjectID]
	if passive == nil {
		definition := e.damage.npc.program.SupportHealerPassive.AtRank(1)
		firstID, reserveErr := e.registry.reserveAbilityObjectIDsLocked(member, definition.MaximumCompanion, 2000)
		if reserveErr != nil {
			return nil, nil, fmt.Errorf("sageIDs: %w", reserveErr)
		}
		objectIDs := make([]uint32, definition.MaximumCompanion)
		for index := range objectIDs {
			objectIDs[index] = firstID + uint32(index)
		}
		run, startErr := newSummonPassiveRun(summonPassiveRunInput{Definition: definition,
			OwnerObjectID: member.deployedObjectID, CompanionObjectIDs: objectIDs})
		if startErr != nil {
			return nil, nil, fmt.Errorf("sageStart: %w", startErr)
		}
		passive = &arenaSagePassive{run: run, startedAt: now}
		world.sages[member.deployedObjectID] = passive
	}
	actors := make(map[uint32]zonecompanion.Actor)
	for _, snapshot := range world.companion.MotionSnapshots(now) {
		actors[snapshot.Actor.ObjectID] = snapshot.Actor
	}
	packets := make([][]byte, 0)
	for _, slot := range passive.run.Slots() {
		if !slot.IsAlive {
			continue
		}
		pet := world.combatPets[slot.ObjectID]
		actor, isActorFound := actors[slot.ObjectID]
		isSeparated := pet != nil && isActorFound && actor.HitPoint > 0 &&
			actor.Position.Sub(game.Vec3(member.playerPosition)).Length() > passive.run.Definition().OwnerDistance
		if pet != nil && !isSeparated {
			continue
		}
		if isSeparated {
			retireRequests, separationErr := passive.run.CompanionSeparated(slot.ObjectID)
			if separationErr != nil {
				return nil, nil, fmt.Errorf("sageSeparate: %w", separationErr)
			}
			for _, req := range retireRequests {
				retirePackets, activation, marshalErr := marshalSummonPassiveWorldRequest(req, raknet.Vector3{})
				if marshalErr != nil {
					return nil, nil, fmt.Errorf("sageSeparationPacket: %w", marshalErr)
				}
				if activation != nil {
					return nil, nil, fmt.Errorf("sageSeparation: unexpected companion spawn")
				}
				packets = append(packets, retirePackets...)
			}
			// The separation requests own deletion; cancel the old attack and
			// motion without queuing another copy of its retirement packet.
			pet.isPublished = false
			pet.retireLocked()
		} else {
			err = passive.run.CompanionDied(slot.ObjectID)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("sageDeath: %w", err)
		}
		replacementID, reserveErr := e.registry.reserveAbilityObjectIDsLocked(member, 1, 2000)
		if reserveErr != nil {
			return nil, nil, fmt.Errorf("sageReplaceID: %w", reserveErr)
		}
		err = passive.run.ReplaceCompanion(slot.ObjectID, replacementID)
		if err != nil {
			return nil, nil, fmt.Errorf("sageReplace: %w", err)
		}
	}
	requests, err := passive.run.Advance(now.Sub(passive.startedAt))
	if err != nil {
		return nil, nil, fmt.Errorf("sageTick: %w", err)
	}
	pets := make([]*arenaCombatPetRun, 0)
	for index, req := range requests {
		if req.Spawn == nil {
			continue
		}
		position, placementErr := summonPassiveSpawnPosition(member.playerPosition, index, len(requests), req.Spawn.SpawnRadius)
		if placementErr != nil {
			return nil, nil, fmt.Errorf("sagePosition: %w", placementErr)
		}
		spawnPackets, spawnErr := companionraknet.Spawn(companionraknet.SpawnRequest{
			ObjectID: req.ObjectID, OwnerObjectID: req.OwnerObjectID, Intent: *req.Spawn,
			Position: game.Vec3(position), ActorTeam: uint8(member.binding.Team)})
		if spawnErr != nil {
			return nil, nil, fmt.Errorf("sageSpawn: %w", spawnErr)
		}
		hitPoint := member.petHitPoint(e.damage.npc.program.NonPlayerHitPoint[util.HashID("HelperMelee")])
		footprint, footprintErr := e.damage.npc.program.FootprintRadius("HelperMelee.Noun")
		if footprintErr != nil {
			return nil, nil, fmt.Errorf("sageFootprint: %w", footprintErr)
		}
		err = world.companion.Put(zonecompanion.Actor{UserID: member.binding.UserID, PeerGeneration: member.generation,
			ObjectID: req.ObjectID, OwnerObjectID: req.OwnerObjectID, Noun: util.HashID(req.Spawn.NounName),
			Position: game.Vec3(position), FootprintRadius: footprint, HitPoint: hitPoint, MaximumHitPoint: hitPoint,
			AggroRadius:  e.damage.npc.program.SupportHealerPetAggroRange,
			IsTargetable: true, IsCombatant: true, IsSageCompanion: true})
		if err != nil {
			return nil, nil, fmt.Errorf("sageActor: %w", err)
		}
		deletePacket, deleteErr := companionraknet.Defeat(req.ObjectID)
		if deleteErr != nil {
			return nil, nil, fmt.Errorf("sageDelete: %w", deleteErr)
		}
		pet := &arenaCombatPetRun{runtime: e.action.ability, packet: packet.Autonomous(), world: world,
			sessionKey: key, gameID: member.binding.GameID, generation: member.generation,
			ownerObjectID: req.OwnerObjectID, objectID: req.ObjectID, kind: arenaCombatPetSage,
			startedAt: now, attackAbility: e.damage.npc.program.SupportHealerPetBasic,
			petDamage:    member.binding.Creatures[member.deployedCreatureIndex].PetDamage,
			deletePacket: deletePacket, isPublished: true}
		if world.combatPets == nil {
			world.combatPets = make(map[uint32]*arenaCombatPetRun)
		}
		world.combatPets[req.ObjectID] = pet
		packets = append(packets, spawnPackets...)
		pets = append(pets, pet)
	}
	return packets, pets, nil
}
