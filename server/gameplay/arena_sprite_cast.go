package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
)

func (e campaignAbilityCommandRuntime) handleArenaSprite(
	packet raknet.Packet, command raknet.ActionCommandData, member gameplayPeerSession,
	abilityID uint32, definition sim.AbilityDefinition,
) ([][]byte, error) {
	if command.Ability == nil || command.Common.Type != raknet.ActionUseCharacterAbility ||
		command.Ability.Index != 3 || abilityID == 0 || definition.Name != "SummonSprite" ||
		definition.Kind != sim.AbilityKindSummonBuff || definition.SpawnNoun != "SpritePet.Noun" ||
		definition.Duration <= 0 || definition.ReleaseDelay <= 0 || definition.PlacementDistance <= 0 ||
		packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
		return e.rejectArenaCharacter(command, "Summon Sprite unavailable")
	}
	sessionKey := packet.Address.String()
	startedAt := e.now()
	e.registry.mutex.Lock()
	source, isFound := e.registry.sessions[sessionKey]
	match := e.registry.arenaMatches[member.binding.GameID]
	isReady := isFound && source.generation == member.generation && source.binding.Mode == game.ModeArena &&
		source.binding.GameID == member.binding.GameID && source.isArenaCombatActive &&
		match != nil && match.isCombatActive && source.stage.IsDungeon() &&
		source.deployedObjectID == command.Common.ObjectID && source.deployedHitPoint() > 0 &&
		source.isAbilityReleaseReady(startedAt)
	if !isReady {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Summon Sprite source unavailable")
	}
	if source.isEnemyStunActive(startedAt) || source.isEnemySilenceActive(startedAt) {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Summon Sprite source controlled")
	}
	err := source.advancePlayerPosition(startedAt, command.Common.Position)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpritePosition: %w", err)
	}
	creature := e.registry.projectPassiveCreature(source, source.deployedCreatureIndex, startedAt)
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpriteTiming: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(projected.ManaCost,
		creature.DamageProfile.PrimaryAttribute, projected.ManaCoefficient,
		source.isOverdriveActiveAt(startedAt))
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpriteMana: %w", err)
	}
	if source.deployedManaPoint() < manaCost ||
		!source.abilityCooldownSession().IsReady(zoneability.HeroAbilityCooldown(abilityID), startedAt) {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Summon Sprite resource unavailable")
	}
	facing, err := geometryraknet.Forward(command.Common.Orientation)
	if err != nil {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Summon Sprite facing unavailable")
	}
	position := game.Vec3(source.playerPosition).Add(game.Vec3(facing).Scale(
		projected.PlacementDistance + spriteFallbackFootprint))
	world, err := e.registry.arenaAbilityWorldLocked(source.binding.GameID)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpriteWorld: %w", err)
	}
	objectID, err := world.objectIDSession.Reserve(1)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpriteID: %w", err)
	}
	hitPoint := e.program.NonPlayerHitPoint[util.HashID("SpritePet")]
	if hitPoint <= 0 {
		hitPoint = spriteFallbackHitPoint
	}
	hitPoint = source.petHitPoint(hitPoint)
	footprint, footprintErr := e.program.FootprintRadius(projected.SpawnNoun)
	if footprintErr != nil || footprint <= 0 {
		footprint = spriteFallbackFootprint
	}
	spawnRun := &heroSummonRun{objectID: objectID, ownerID: command.Common.ObjectID,
		hitPoint: hitPoint, definition: projected}
	spawnPackets, err := marshalHeroSummonSpawnForTeam(spawnRun, position, footprint, uint8(source.binding.Team))
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpriteSpawn: %w", err)
	}
	deletePacket, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{objectID}})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpriteDelete: %w", err)
	}
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: abilityID, AbilityIndex: command.Ability.Index,
		SourceStartMilliseconds:  packet.SourceTime,
		SourceCommitMilliseconds: packet.SourceTime,
		SourceEndMilliseconds:    packet.SourceTime + uint64(projected.ReleaseDelay/time.Millisecond),
	})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpriteAck: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(command.Common.Unknown[0], abilityID,
		command.Ability.Index, packet.SourceTime, 0, projected.ReleaseDelay)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpriteRelease: %w", err)
	}
	remainingMana := source.deployedManaPoint() - manaCost
	startPackets, err := abilityraknet.StartSpendPresentation(command.Common.ObjectID, abilityID,
		projected.AnimationName, projected.Cooldown, packet.SourceTime, remainingMana)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpriteStart: %w", err)
	}
	stopPackets, err := marshalZonePlayerStop(command.Common.ObjectID, source.playerPosition)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpriteStop: %w", err)
	}
	cooldownReservation, isReserved := source.abilityCooldownSession().Reserve(
		zoneability.HeroAbilityCooldown(abilityID), startedAt, projected.Cooldown)
	if !isReserved {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Summon Sprite cooldown unavailable")
	}
	releaseReservation, isReserved := source.abilityReleaseSession().Reserve(startedAt, projected.ReleaseDelay)
	if !isReserved {
		source.abilityCooldownSession().Rollback(cooldownReservation)
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Summon Sprite release unavailable")
	}
	previousMana := source.deployedManaPoint()
	err = source.stopPlayerMovement(startedAt)
	if err == nil {
		err = source.setDeployedManaPoints(remainingMana)
	}
	if err != nil {
		source.abilityCooldownSession().Rollback(cooldownReservation)
		source.abilityReleaseSession().Rollback(releaseReservation)
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpriteCommit: %w", err)
	}
	err = world.companion.Put(zonecompanion.Actor{
		UserID: source.binding.UserID, PeerGeneration: source.generation,
		ObjectID: objectID, OwnerObjectID: command.Common.ObjectID,
		Noun: util.HashID(projected.SpawnNoun), Position: position,
		FootprintRadius: footprint, HitPoint: hitPoint, MaximumHitPoint: hitPoint,
		MovementSpeed: spriteMovementSpeed, FollowDistance: spriteFollowDistance,
		IsTargetable: true, IsCombatant: false,
	})
	if err != nil {
		manaErr := source.setDeployedManaPoints(previousMana)
		if manaErr != nil && e.logger != nil {
			e.logger.Printf("Arena Sprite payment rollback failed: %v", manaErr)
		}
		source.abilityCooldownSession().Rollback(cooldownReservation)
		source.abilityReleaseSession().Rollback(releaseReservation)
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpriteCompanion: %w", err)
	}
	run := &arenaSpriteRun{runtime: e, packet: packet, world: world,
		sessionKey: sessionKey, gameID: source.binding.GameID, generation: source.generation,
		ownerObjectID: command.Common.ObjectID, objectID: objectID, startedAt: startedAt,
		definition: projected, releasePacket: releasePacket, deletePacket: deletePacket}
	if world.sprites == nil {
		world.sprites = make(map[uint32]*arenaSpriteRun)
	}
	world.sprites[objectID] = run
	e.registry.sessions[sessionKey] = source
	e.registry.mutex.Unlock()
	producers := []raknet.ScheduledPacketProducer{{Delay: projected.ReleaseDelay, Produce: run.release}}
	for delay := projected.ReleaseDelay; delay < projected.Duration; delay += spriteHealingInterval {
		producers = append(producers, raknet.ScheduledPacketProducer{Delay: delay, Produce: run.sting})
		if delay+spriteHealingHitDelay < projected.Duration {
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: delay + spriteHealingHitDelay, Produce: run.tick})
		}
		if delay+time.Second < projected.Duration {
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: delay + time.Second, Produce: run.resumeFollow})
		}
	}
	producers = append(producers, raknet.ScheduledPacketProducer{Delay: projected.Duration, Produce: run.expire})
	producers = e.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(producers, run.fail)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err == nil && cancel == nil {
		err = errors.New("Summon Sprite cancellation unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		e.registry.mutex.Lock()
		latest, isCurrent := run.isCurrentLocked()
		if isCurrent {
			delete(world.sprites, objectID)
			world.companion.Remove(objectID)
			latest.abilityCooldownSession().Rollback(cooldownReservation)
			latest.abilityReleaseSession().Rollback(releaseReservation)
			manaErr := latest.setDeployedManaPoints(previousMana)
			if manaErr != nil && e.logger != nil {
				e.logger.Printf("Arena Sprite schedule payment rollback failed: %v", manaErr)
			}
			e.registry.sessions[sessionKey] = latest
		}
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpriteSchedule: %w", err)
	}
	run.cancel = cancel
	response := append([][]byte{ackPacket}, startPackets...)
	response = append(response, stopPackets...)
	response = append(response, spawnPackets...)
	e.registry.mutex.Lock()
	_, isCurrent := run.isCurrentLocked()
	if isCurrent {
		run.isPublished = true
	}
	e.registry.mutex.Unlock()
	return response, nil
}
