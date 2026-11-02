package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

func (e campaignAbilityCommandRuntime) handleArenaRootingPlague(packet raknet.Packet,
	command raknet.ActionCommandData, member gameplayPeerSession, abilityID uint32,
	definition sim.AbilityDefinition,
) ([][]byte, error) {
	if command.Ability == nil || command.Common.Type != raknet.ActionUseCharacterAbility ||
		definition.Name != "RootingPlague" || definition.Kind != sim.AbilityKindInfection ||
		definition.Range <= 0 || definition.Radius <= 0 || definition.Duration <= 0 ||
		definition.TickDuration <= 0 || definition.NumberOfTicks == 0 ||
		definition.RootModifierID == 0 || definition.SecondaryModifierID == 0 ||
		definition.SpreadModifierID == 0 {
		return e.rejectArenaCharacter(command, "Rooting Plague definition unavailable")
	}
	if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
		return e.rejectArenaCharacter(command, "Rooting Plague schedule unavailable")
	}
	sessionKey := packet.Address.String()
	startedAt := e.now()
	e.registry.mutex.Lock()
	source, isFound := e.registry.sessions[sessionKey]
	match := e.registry.arenaMatches[member.binding.GameID]
	isReady := isFound && source.generation == member.generation && source.binding.Mode == game.ModeArena &&
		source.binding.GameID == member.binding.GameID && source.isArenaCombatActive && source.stage.IsDungeon() &&
		match != nil && match.isCombatActive && match.random != nil && e.registry.timer != nil &&
		source.deployedObjectID == command.Common.ObjectID && source.deployedHitPoint() > 0 &&
		source.isAbilityReleaseReady(startedAt)
	if !isReady {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Rooting Plague source unavailable")
	}
	if source.isEnemyStunActive(startedAt) || source.isEnemySilenceActive(startedAt) {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Rooting Plague source controlled")
	}
	err := source.advancePlayerPosition(startedAt, raknet.Vector3{})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("plagueSourcePosition: %w", err)
	}
	creature := e.registry.projectPassiveCreature(source, source.deployedCreatureIndex, startedAt)
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("plagueTiming: %w", err)
	}
	targets := e.registry.abilityTargetsLocked(e, source, projected, packet.SourceTime).(*arenaAbilityTargets)
	targetObjectID := command.Ability.TargetID
	if targetObjectID == 0 {
		targetObjectID = zoneability.CursorTarget(targets, source.deployedObjectID,
			game.Vec3(command.Ability.CursorPosition), game.Vec3(command.Ability.TargetPosition), campaignHeldMeleeCursorRadius)
	}
	target, isTargetFound := targets.NPC(targetObjectID)
	if !isTargetFound || !isInsideZoneTrigger(source.playerPosition, raknet.Vector3(target.Plan.Position),
		heroAbilityAdmissionRange(creature, projected)) {
		e.registry.mutex.Unlock()
		targets.finish()
		return e.rejectArenaCharacter(command, "Rooting Plague target unavailable")
	}
	world, err := e.registry.arenaInfectionWorldLocked(source.binding.GameID)
	if err != nil {
		e.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("plagueWorld: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(projected.ManaCost, creature.DamageProfile.PrimaryAttribute,
		projected.ManaCoefficient, source.isOverdriveActiveAt(startedAt))
	if err != nil {
		e.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("plagueMana: %w", err)
	}
	if source.deployedManaPoint() < manaCost {
		e.registry.mutex.Unlock()
		targets.finish()
		return e.rejectArenaCharacter(command, "Rooting Plague power unavailable")
	}
	cooldown, err := zoneability.ProjectCooldown(creature, projected)
	if err != nil {
		e.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("plagueCooldown: %w", err)
	}
	projected.Cooldown = cooldown
	start, err := abilityraknet.StartAreaBasic(abilityraknet.AreaBasicStartRequest{
		SyncStamp: command.Common.Unknown[0], SourceID: source.deployedObjectID,
		AbilityID: abilityID, AbilityIndex: command.Ability.Index, SourceTime: packet.SourceTime,
		AnimationName: projected.AnimationName, MuzzleEffectName: projected.MuzzleEffectName,
		HitDelay: projected.HitDelay, ReleaseDelay: projected.ReleaseDelay, Cooldown: cooldown,
	})
	if err != nil {
		e.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("plagueStart: %w", err)
	}
	previousMana := source.deployedManaPoint()
	remainingMana := previousMana - manaCost
	manaPacket, err := abilityraknet.Mana(source.deployedObjectID, remainingMana)
	if err != nil {
		e.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("plagueManaPacket: %w", err)
	}
	cooldownReservation, isReserved := source.abilityCooldownSession().Reserve(zoneability.HeroAbilityCooldown(abilityID), startedAt, cooldown)
	if !isReserved {
		e.registry.mutex.Unlock()
		targets.finish()
		return e.rejectArenaCharacter(command, "Rooting Plague cooldown unavailable")
	}
	releaseReservation, isReserved := source.abilityReleaseSession().Reserve(startedAt, projected.ReleaseDelay)
	if !isReserved {
		source.abilityCooldownSession().Rollback(cooldownReservation)
		e.registry.mutex.Unlock()
		targets.finish()
		return e.rejectArenaCharacter(command, "Rooting Plague release unavailable")
	}
	err = source.stopPlayerMovement(startedAt)
	if err == nil {
		err = source.setDeployedManaPoints(remainingMana)
	}
	if err != nil {
		source.abilityCooldownSession().Rollback(cooldownReservation)
		source.abilityReleaseSession().Rollback(releaseReservation)
		e.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("plagueCommit: %w", err)
	}
	e.registry.sessions[sessionKey] = source
	run := &arenaInfectionRun{runtime: e, packet: packet, world: world,
		sessionKey: sessionKey, gameID: source.binding.GameID, generation: source.generation,
		sourceObjectID: source.deployedObjectID, source: source, startedAt: startedAt, creature: creature,
		definition: projected, abilityID: abilityID, releasePacket: start.Release,
		entries: make(map[uint32]*arenaInfectionEntry)}
	world.runs[run] = struct{}{}
	err = run.admitEntryLocked(targetObjectID, true, startedAt, packet.SourceTime)
	if err != nil {
		delete(world.runs, run)
		source.abilityCooldownSession().Rollback(cooldownReservation)
		source.abilityReleaseSession().Rollback(releaseReservation)
		manaErr := source.setDeployedManaPoints(previousMana)
		if manaErr != nil && e.logger != nil {
			e.logger.Printf("Arena Rooting Plague mana rollback failed: %v", manaErr)
		}
		e.registry.sessions[sessionKey] = source
		e.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("plaguePrimary: %w", err)
	}
	e.registry.mutex.Unlock()
	targets.finish()
	tickCancel, err := e.registry.timer.Schedule(projected.TickDuration, run.tick)
	if err == nil && tickCancel == nil {
		err = errors.New("Rooting Plague tick cancellation unavailable")
	}
	if err != nil {
		if tickCancel != nil {
			tickCancel()
		}
		run.fail(err)
		return nil, fmt.Errorf("plagueTickSchedule: %w", err)
	}
	e.registry.mutex.Lock()
	if _, isRunFound := world.runs[run]; !isRunFound {
		e.registry.mutex.Unlock()
		tickCancel()
		return nil, errors.New("Rooting Plague retired during admission")
	}
	run.tickCancel = tickCancel
	e.registry.mutex.Unlock()
	producers := e.registry.producerGuard.scheduledProducers(sessionKey,
		[]raknet.ScheduledPacketProducer{{Delay: projected.ReleaseDelay, Produce: run.release}})
	var releaseCancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		releaseCancel, err = packet.ScheduleGroupResult(producers, run.fail)
	} else {
		releaseCancel, err = packet.ScheduleGroup(producers)
	}
	if err == nil && releaseCancel == nil {
		err = errors.New("Rooting Plague release cancellation unavailable")
	}
	if err != nil {
		if releaseCancel != nil {
			releaseCancel()
		}
		run.fail(err)
		return nil, fmt.Errorf("plagueReleaseSchedule: %w", err)
	}
	e.registry.mutex.Lock()
	if _, isRunFound := world.runs[run]; !isRunFound {
		e.registry.mutex.Unlock()
		releaseCancel()
		return nil, errors.New("Rooting Plague retired during release admission")
	}
	run.releaseCancel = releaseCancel
	run.isAccepted = true
	e.registry.mutex.Unlock()
	return append([][]byte{start.Acknowledge, manaPacket}, start.Presentation...), nil
}

func (e *arenaInfectionRun) release() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	_, isCurrent := e.currentLocked()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.releasePacket}, nil
}

func (e *arenaInfectionRun) fail(cause error) {
	e.runtime.registry.mutex.Lock()
	e.retireLocked()
	e.retireRootsLocked()
	e.runtime.registry.mutex.Unlock()
	if e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena Rooting Plague schedule stopped owner=%d: %v", e.sourceObjectID, cause)
	}
}
