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
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

func (e campaignAbilityCommandRuntime) handleArenaQuantumBlink(
	packet raknet.Packet, command raknet.ActionCommandData, member gameplayPeerSession,
	abilityID uint32, definition sim.AbilityDefinition,
) ([][]byte, error) {
	if command.Ability == nil || command.Common.Type != raknet.ActionUseCharacterAbility ||
		command.Ability.Index != 2 || abilityID == 0 || definition.Name != "QuantumBlink" ||
		definition.Kind != sim.AbilityKindQuantumBlink || definition.Range <= 0 || definition.Radius <= 0 ||
		definition.HitDelay <= 0 || definition.ReleaseDelay <= definition.HitDelay ||
		definition.ShotCount != 5 || definition.TickDuration <= 0 ||
		len(definition.AnimationNames) != 4 || definition.SlideAnimationName == "" ||
		definition.AnimationName == "" || definition.HitEffectName == "" ||
		(packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil) {
		return e.rejectArenaCharacter(command, "Quantum Blink unavailable")
	}
	sessionKey := packet.Address.String()
	e.registry.mutex.Lock()
	startedAt := e.now()
	source, isFound := e.registry.sessions[sessionKey]
	match := e.registry.arenaMatches[member.binding.GameID]
	isReady := isFound && source.generation == member.generation && source.binding.Mode == game.ModeArena &&
		source.binding.GameID == member.binding.GameID && source.isArenaCombatActive && source.stage.IsDungeon() &&
		match != nil && match.isCombatActive && match.navigation != nil && match.random != nil &&
		source.deployedObjectID == command.Common.ObjectID && source.deployedHitPoint() > 0 &&
		source.isAbilityReleaseReady(startedAt) && source.heroQuantumBlink == nil
	if !isReady {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Quantum Blink source unavailable")
	}
	if source.isEnemyStunActive(startedAt) || source.isEnemySilenceActive(startedAt) {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Quantum Blink source controlled")
	}
	err := source.advancePlayerPosition(startedAt, command.Common.Position)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaQuantumPosition: %w", err)
	}
	cursor := command.Ability.TargetPosition
	if !isReportedZonePosition(cursor) {
		cursor = command.Ability.CursorPosition
	}
	creature := e.registry.projectPassiveCreature(source, source.deployedCreatureIndex, startedAt)
	if !isReportedZonePosition(cursor) || !isFiniteZonePosition(cursor) ||
		!isInsideZoneTrigger(source.playerPosition, cursor, heroAbilityAdmissionRange(creature, definition)) {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Quantum Blink cursor unavailable")
	}
	targets := e.registry.abilityTargetsLocked(e, source, definition, packet.SourceTime).(*arenaAbilityTargets)
	targetObjectIDs := make([]uint32, 0)
	initialTargetIndex := -1
	selectedDistance := definition.Radius
	for _, target := range targets.LiveSnapshots() {
		distance := game.Vec3(cursor).Sub(target.Plan.Position).Length()
		if distance > definition.Radius {
			continue
		}
		targetObjectIDs = append(targetObjectIDs, target.Plan.ObjectID)
		if initialTargetIndex < 0 || distance < selectedDistance {
			initialTargetIndex = len(targetObjectIDs) - 1
			selectedDistance = distance
		}
	}
	initialDestination := cursor
	if initialTargetIndex >= 0 {
		target, isTargetFound := targets.NPC(targetObjectIDs[initialTargetIndex])
		if isTargetFound {
			initialDestination = quantumBlinkSlideDestination(source.playerPosition,
				raknet.Vector3(target.Plan.Position), source.deployedCampaignFootprintRadius(),
				target.Plan.ActorFootprintRadius())
		}
	}
	err = zonenavigation.ValidateMovement(match.navigation, game.Vec3(source.playerPosition),
		game.Vec3(initialDestination), source.deployedCampaignNavigationRadius())
	if err != nil {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Quantum Blink destination unavailable")
	}
	damage, err := zoneability.ProjectDamage(creature, definition, definition.MinimumDamage, definition.MaximumDamage)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaQuantumDamage: %w", err)
	}
	cooldown, err := zoneability.ProjectCooldown(creature, definition)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaQuantumCooldown: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(definition.ManaCost,
		creature.DamageProfile.PrimaryAttribute, definition.ManaCoefficient,
		source.isOverdriveActiveAt(startedAt))
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaQuantumMana: %w", err)
	}
	if source.deployedManaPoint() < manaCost {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Quantum Blink power unavailable")
	}
	slideDistance := game.Vec3(source.playerPosition).Sub(game.Vec3(initialDestination)).Length()
	slideDelay := definition.HitDelay + time.Duration(float64(slideDistance)/100*float64(time.Second))
	releaseDelay := slideDelay + time.Duration(definition.ShotCount)*definition.TickDuration
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: abilityID, AbilityIndex: command.Ability.Index,
		SourceStartMilliseconds:  packet.SourceTime,
		SourceCommitMilliseconds: packet.SourceTime + uint64(slideDelay/time.Millisecond),
		SourceEndMilliseconds:    packet.SourceTime + uint64(releaseDelay/time.Millisecond),
	})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaQuantumAck: %w", err)
	}
	remainingMana := source.deployedManaPoint() - manaCost
	startPackets, err := abilityraknet.StartSpendPresentation(command.Common.ObjectID, abilityID,
		definition.AnimationName, cooldown, packet.SourceTime, remainingMana)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaQuantumStart: %w", err)
	}
	cooldownReservation, isReserved := source.abilityCooldownSession().Reserve(
		zoneability.HeroAbilityCooldown(abilityID), startedAt, cooldown)
	if !isReserved {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Quantum Blink cooldown unavailable")
	}
	releaseReservation, isReserved := source.abilityReleaseSession().Reserve(startedAt, releaseDelay)
	if !isReserved {
		source.abilityCooldownSession().Rollback(cooldownReservation)
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Quantum Blink release unavailable")
	}
	previousMotion := source.playerMotionSnapshot()
	previousMana := source.deployedManaPoint()
	err = source.stopPlayerMovement(startedAt)
	if err == nil {
		err = source.setDeployedManaPoints(remainingMana)
	}
	if err != nil {
		source.restorePlayerMotion(previousMotion, source.playerMotionRevision())
		source.abilityCooldownSession().Rollback(cooldownReservation)
		source.abilityReleaseSession().Rollback(releaseReservation)
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaQuantumCommit: %w", err)
	}
	blink := &heroQuantumBlinkRun{registry: e.registry,
		returnPosition: initialDestination, sourceObjectID: command.Common.ObjectID,
		startedAt: startedAt, sourceTime: packet.SourceTime,
		orientation: normalizedHeroOrientation(command.Common.Orientation)}
	source.heroQuantumBlink = blink
	e.registry.sessions[sessionKey] = source
	e.registry.mutex.Unlock()
	run := &arenaQuantumRun{runtime: e, packet: packet, sessionKey: sessionKey,
		gameID: source.binding.GameID, generation: source.generation,
		objectID: command.Common.ObjectID, abilityID: abilityID, index: command.Ability.Index,
		syncStamp: command.Common.Unknown[0], creature: creature, definition: definition,
		damage: damage, blink: blink, targetObjectIDs: targetObjectIDs,
		initialTargetIndex: initialTargetIndex, initialDestination: initialDestination,
		slideDelay: slideDelay, releaseDelay: releaseDelay, releaseReservation: releaseReservation}
	producers := make([]raknet.ScheduledPacketProducer, 0, definition.ShotCount+3)
	producers = append(producers, raknet.ScheduledPacketProducer{Delay: definition.HitDelay, Produce: run.slide})
	for index := uint32(0); index <= definition.ShotCount; index++ {
		deadline := slideDelay
		if index > 0 {
			deadline += time.Duration(index-1) * definition.TickDuration
		}
		step := arenaQuantumStep{run: run, index: index, deadline: deadline}
		producers = append(producers, raknet.ScheduledPacketProducer{Delay: deadline, Produce: step.produce})
	}
	producers = append(producers, raknet.ScheduledPacketProducer{Delay: releaseDelay, Produce: run.finish})
	producers = e.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(producers, run.fail)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err == nil && cancel == nil {
		err = errors.New("Quantum Blink cancellation unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		e.registry.mutex.Lock()
		latest, isCurrent := run.currentLocked()
		if isCurrent {
			latest.heroQuantumBlink = nil
			blink.Stop()
			latest.restorePlayerMotion(previousMotion, latest.playerMotionRevision())
			latest.abilityCooldownSession().Rollback(cooldownReservation)
			latest.abilityReleaseSession().Rollback(releaseReservation)
			manaErr := latest.setDeployedManaPoints(previousMana)
			if manaErr != nil && e.logger != nil {
				e.logger.Printf("Arena Quantum Blink mana rollback failed owner=%d: %v", run.objectID, manaErr)
			}
			e.registry.sessions[sessionKey] = latest
		}
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaQuantumSchedule: %w", err)
	}
	blink.mutex.Lock()
	if !blink.isStopped {
		blink.cancel = cancel
	} else {
		cancel()
	}
	blink.mutex.Unlock()
	response := append([][]byte{ackPacket}, startPackets...)
	return response, nil
}
