package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e campaignAbilityCommandRuntime) handleArenaHeroCharge(
	packet raknet.Packet, command raknet.ActionCommandData, member gameplayPeerSession,
	abilityID uint32, definition sim.AbilityDefinition,
) ([][]byte, error) {
	isPhantom := definition.Name == "PhantomCharge" && command.Ability != nil && command.Ability.Index == 3
	isRush := definition.Name == "EntanglingRush" && command.Ability != nil && command.Ability.Index == 2
	isBeast := definition.Name == "BeastCharge" && command.Ability != nil && command.Ability.Index == 2
	isRoar := definition.Name == "BioRandom2" && command.Ability != nil && command.Ability.Index == 3
	if command.Ability == nil || command.Common.Type != raknet.ActionUseCharacterAbility ||
		(!isPhantom && !isRush && !isBeast && !isRoar) ||
		definition.Kind != sim.AbilityKindCharge || definition.RootModifierID == 0 ||
		definition.StatusDuration <= 0 || definition.MovementSpeedMultiplier <= 0 ||
		(isRush && (definition.OutAnimationName == "" || definition.MuzzleEffectName == "")) {
		return e.rejectArenaCharacter(command, "Arena charge definition unavailable")
	}
	if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
		return e.rejectArenaCharacter(command, "Arena charge schedule unavailable")
	}
	sessionKey := packet.Address.String()
	startedAt := e.now()
	e.registry.mutex.Lock()
	source, isFound := e.registry.sessions[sessionKey]
	match := e.registry.arenaMatches[member.binding.GameID]
	isReady := isFound && source.generation == member.generation && source.binding.Mode == game.ModeArena &&
		source.binding.GameID == member.binding.GameID && source.isArenaCombatActive && source.stage.IsDungeon() &&
		match != nil && match.isCombatActive && match.navigation != nil &&
		source.deployedObjectID == command.Common.ObjectID && source.deployedHitPoint() > 0 &&
		source.isAbilityReleaseReady(startedAt)
	if !isReady {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Arena charge source unavailable")
	}
	if source.isEnemyStunActive(startedAt) || source.isEnemySilenceActive(startedAt) {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Arena charge source controlled")
	}
	err := source.advancePlayerPosition(startedAt, raknet.Vector3{})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaChargePosition: %w", err)
	}
	position := command.Ability.CursorPosition
	if !isReportedZonePosition(position) {
		position = command.Ability.TargetPosition
	}
	var beastTarget zonenpc.Snapshot
	if isBeast || isRoar {
		targets := e.registry.abilityTargetsLocked(e, source, definition, packet.SourceTime).(*arenaAbilityTargets)
		beastTarget, isFound = targets.NPC(command.Ability.TargetID)
		if !isFound {
			e.registry.mutex.Unlock()
			return e.rejectArenaCharacter(command, "Arena charge target unavailable")
		}
		position = raknet.Vector3(beastTarget.Plan.Position)
	}
	creature := e.registry.projectPassiveCreature(source, source.deployedCreatureIndex, startedAt)
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaChargeTiming: %w", err)
	}
	// Build-103 charge catalogs carry modifier IDs without generic status kinds.
	if isPhantom {
		projected.StatusKind = sim.AbilityStatusKindSilence
	} else if isBeast {
		projected.StatusKind = sim.AbilityStatusKindStun
	} else if !isRoar {
		projected.StatusKind = arenaChargeRoot
	} else {
		projected.StatusKind = sim.AbilityStatusKindTaunt
	}
	if !isReportedZonePosition(position) || !isFiniteZonePosition(position) ||
		chargeDistance(source.playerPosition, position) > heroAbilityAdmissionRange(creature, projected) {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Arena charge position unavailable")
	}
	destination := position
	if isRush || isRoar {
		destination = chargeDestination(source.playerPosition, position, projected.Distance)
	} else if isBeast {
		stopDistance := source.deployedCampaignActorFootprintRadius() +
			max(float32(0.5), beastTarget.Plan.ActorFootprintRadius()) + 0.2
		destination = chargeDestination(source.playerPosition, position, stopDistance)
	}
	err = zonenavigation.ValidateMovement(match.navigation, game.Vec3(source.playerPosition),
		game.Vec3(destination), source.deployedCampaignNavigationRadius())
	if err != nil {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Arena charge destination unreachable")
	}
	damage := game.DamageRange{}
	if !isRoar {
		damage, err = zoneability.ProjectDamage(creature, projected, projected.MinimumDamage, projected.MaximumDamage)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaChargeDamageProject: %w", err)
		}
	}
	cooldown, err := zoneability.ProjectCooldown(creature, projected)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaChargeCooldown: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(projected.ManaCost, creature.DamageProfile.PrimaryAttribute,
		projected.ManaCoefficient, source.isOverdriveActiveAt(startedAt))
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaChargeManaCost: %w", err)
	}
	if source.deployedManaPoint() < manaCost {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Arena charge power unavailable")
	}
	travel := time.Duration(float64(chargeDistance(source.playerPosition, destination)) /
		float64(zonePlayerMoveSpeed*projected.MovementSpeedMultiplier) * float64(time.Second))
	impactDelay := projected.HitDelay + max(travel, 50*time.Millisecond)
	releaseDelay := impactDelay + projected.ReleaseDelay
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: abilityID, AbilityIndex: command.Ability.Index,
		SourceStartMilliseconds:  packet.SourceTime,
		SourceCommitMilliseconds: packet.SourceTime + uint64(projected.HitDelay/time.Millisecond),
		SourceEndMilliseconds:    packet.SourceTime + uint64(releaseDelay/time.Millisecond),
	})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaChargeAck: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(command.Common.Unknown[0], abilityID,
		command.Ability.Index, packet.SourceTime, projected.HitDelay, releaseDelay)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaChargeRelease: %w", err)
	}
	remainingMana := source.deployedManaPoint() - manaCost
	startPackets, err := abilityraknet.StartSpendPresentation(command.Common.ObjectID, abilityID,
		projected.AnimationName, cooldown, packet.SourceTime, remainingMana)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaChargeStart: %w", err)
	}
	speedPacket, err := heroChargeSpeedPacket(command.Common.ObjectID,
		zonePlayerMoveSpeed*projected.MovementSpeedMultiplier)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaChargeSpeedStart: %w", err)
	}
	// Build-103 owned Arena actors did not follow ChargeMove's ObjectPlayerMove
	// goal on either peer. The native flat ObjectJump mover advances both views.
	startMovementPackets, err := marshalZonePlayerStop(command.Common.ObjectID, source.playerPosition)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaChargeMovement: %w", err)
	}
	if projected.HitDelay == 0 {
		slidePacket, slideErr := actionraknet.ChargeSlide(command.Common.ObjectID,
			game.Vec3(source.playerPosition), game.Vec3(destination),
			zonePlayerMoveSpeed*projected.MovementSpeedMultiplier)
		if slideErr != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaChargeImmediateSlide: %w", slideErr)
		}
		startMovementPackets = append(startMovementPackets, slidePacket)
	}
	var effectPacket []byte
	var removePackets [][]byte
	var effectSlot uint8
	if isPhantom {
		effectPacket, removePackets, effectSlot, err = preparePhantomChargeEffect(e.effectPool,
			command.Common.ObjectID, projected.ActivationEffectName)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaChargeEffect: %w", err)
		}
	}
	world, err := e.registry.arenaAbilityWorldLocked(source.binding.GameID)
	if err != nil {
		if isPhantom {
			e.effectPool.Release(command.Common.ObjectID, effectSlot)
		}
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaChargeWorld: %w", err)
	}
	if world.charges[command.Common.ObjectID] != nil {
		if isPhantom {
			e.effectPool.Release(command.Common.ObjectID, effectSlot)
		}
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Arena charge already active")
	}
	cooldownReservation, isReserved := source.abilityCooldownSession().Reserve(
		zoneability.HeroAbilityCooldown(abilityID), startedAt, cooldown)
	if !isReserved {
		if isPhantom {
			e.effectPool.Release(command.Common.ObjectID, effectSlot)
		}
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Arena charge cooldown unavailable")
	}
	releaseReservation, isReserved := source.abilityReleaseSession().Reserve(startedAt, releaseDelay)
	if !isReserved {
		source.abilityCooldownSession().Rollback(cooldownReservation)
		if isPhantom {
			e.effectPool.Release(command.Common.ObjectID, effectSlot)
		}
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Arena charge release unavailable")
	}
	previousMana := source.deployedManaPoint()
	err = source.stopPlayerMovement(startedAt)
	if err == nil {
		err = source.setDeployedManaPoints(remainingMana)
	}
	if err != nil {
		source.abilityCooldownSession().Rollback(cooldownReservation)
		source.abilityReleaseSession().Rollback(releaseReservation)
		if isPhantom {
			e.effectPool.Release(command.Common.ObjectID, effectSlot)
		}
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaChargeCommit: %w", err)
	}
	run := &arenaHeroChargeRun{runtime: e, packet: packet, world: world,
		sessionKey: sessionKey, gameID: source.binding.GameID, generation: source.generation,
		objectID: command.Common.ObjectID, abilityID: abilityID, start: source.playerPosition, destination: destination,
		targetPosition: position, targetObjectID: command.Ability.TargetID,
		creature: creature, definition: projected, damage: damage, impactDelay: impactDelay,
		releaseDelay: releaseDelay, releasePacket: releasePacket,
		effectSlot: effectSlot, startedAt: startedAt}
	if isPhantom {
		run.removePacket = removePackets[0]
	}
	if world.charges == nil {
		world.charges = make(map[uint32]*arenaHeroChargeRun)
	}
	world.charges[run.objectID] = run
	e.registry.sessions[sessionKey] = source
	e.registry.mutex.Unlock()
	producers := []raknet.ScheduledPacketProducer{
		{Delay: impactDelay, Produce: run.impact},
		{Delay: releaseDelay, Produce: run.release},
	}
	if projected.HitDelay > 0 {
		producers = append(producers, raknet.ScheduledPacketProducer{Delay: projected.HitDelay, Produce: run.slide})
	}
	producers = e.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(producers, run.fail)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err == nil && cancel == nil {
		err = errors.New("Arena charge cancellation unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		e.registry.mutex.Lock()
		latest, isCurrent := run.currentLocked()
		if isCurrent {
			delete(world.charges, run.objectID)
			latest.abilityCooldownSession().Rollback(cooldownReservation)
			latest.abilityReleaseSession().Rollback(releaseReservation)
			manaErr := latest.setDeployedManaPoints(previousMana)
			if manaErr != nil && e.logger != nil {
				e.logger.Printf("Arena hero charge payment rollback failed: %v", manaErr)
			}
			e.registry.sessions[sessionKey] = latest
		}
		if isPhantom {
			e.effectPool.Release(run.objectID, effectSlot)
		}
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaChargeSchedule: %w", err)
	}
	run.cancel = cancel
	response := append([][]byte{ackPacket}, startPackets...)
	response = append(response, speedPacket)
	response = append(response, startMovementPackets...)
	if len(effectPacket) != 0 {
		response = append(response, effectPacket)
	}
	return response, nil
}
