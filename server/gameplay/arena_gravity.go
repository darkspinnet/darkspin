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
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type arenaGravityStormRun struct {
	runtime           campaignAbilityCommandRuntime
	packet            raknet.Packet
	world             *arenaAbilityWorld
	sessionKey        string
	gameID            uint32
	generation        uint64
	objectID          uint32
	effectObjectID    uint32
	effectSlot        uint8
	center            raknet.Vector3
	creature          game.GameplayCreature
	definition        sim.AbilityDefinition
	rootDuration      time.Duration
	slamStart         time.Duration
	slamHit           time.Duration
	releaseAt         time.Duration
	cleanupAt         time.Duration
	releasePacket     []byte
	targetIDs         []uint32
	cancel            raknet.CancelSchedule
	isEffectPublished bool
	isEffectBound     bool
	isEffectDeleted   bool
	startedAt         time.Time
}

func (e *arenaGravityStormRun) currentLocked() (gameplayPeerSession, bool) {
	source, isFound := e.runtime.registry.sessions[e.sessionKey]
	match := e.runtime.registry.arenaMatches[e.gameID]
	isCurrent := isFound && !e.runtime.registry.isClosed && match != nil && match.isCombatActive &&
		match.abilityWorld == e.world && e.world.gravityStorms[e.objectID] == e &&
		source.generation == e.generation && source.binding.Mode == game.ModeArena &&
		source.binding.GameID == e.gameID && source.isArenaCombatActive &&
		source.deployedObjectID == e.objectID && source.deployedHitPoint() > 0
	return source, isCurrent
}

func (e *arenaGravityStormRun) retireLocked() {
	if e.world.gravityStorms[e.objectID] != e {
		return
	}
	delete(e.world.gravityStorms, e.objectID)
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	packets := make([][]byte, 0, 4)
	if e.isEffectPublished && e.isEffectBound {
		packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: e.effectSlot + 1, ObjectID: e.effectObjectID,
			IsRemovalRequested: true, IsHardStop: true,
		})
		if err != nil {
			if e.runtime.logger != nil {
				e.runtime.logger.Printf("Gravity Storm effect retirement failed owner=%d: %v", e.objectID, err)
			}
		} else {
			packets = append(packets, packet)
		}
	}
	if e.isEffectPublished && !e.isEffectDeleted {
		packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.effectObjectID}})
		if err != nil {
			if e.runtime.logger != nil {
				e.runtime.logger.Printf("Gravity Storm object retirement failed owner=%d: %v", e.objectID, err)
			}
		} else {
			packets = append(packets, packet)
		}
	}
	if e.isEffectBound {
		e.runtime.effectPool.Release(e.effectObjectID, e.effectSlot)
		e.isEffectBound = false
	}
	for _, objectID := range e.targetIDs {
		for key, member := range e.runtime.registry.sessions {
			if member.binding.Mode != game.ModeArena || member.binding.GameID != e.gameID || member.deployedObjectID != objectID {
				continue
			}
			status := member.arenaHitStatuses[arenaHitStatusKey{modifierID: e.definition.RootModifierID}]
			if status != nil && status.sourceObjectID == e.objectID && status.kind == sim.AbilityStatusKindStun {
				status.retire(&member)
				e.runtime.registry.sessions[key] = member
			}
		}
		packet, err := npcraknet.ResetAnimation(objectID, e.packet.SourceTime+uint64(e.runtime.now().Sub(e.startedAt)/time.Millisecond))
		if err != nil {
			if e.runtime.logger != nil {
				e.runtime.logger.Printf("Gravity Storm target reset failed target=%d: %v", objectID, err)
			}
		} else {
			packets = append(packets, packet)
		}
	}
	packet, err := abilityraknet.AnimationReset(e.objectID, e.packet.SourceTime+uint64(e.runtime.now().Sub(e.startedAt)/time.Millisecond))
	if err != nil {
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("Gravity Storm caster reset failed owner=%d: %v", e.objectID, err)
		}
	} else {
		packets = append(packets, packet)
	}
	e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, packets)
}

func (e *gameplaySessionRegistry) retireArenaGravityStormsLocked(gameID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil {
		return
	}
	for _, run := range match.abilityWorld.gravityStorms {
		run.retireLocked()
	}
}

func (e *gameplaySessionRegistry) retireArenaGravityStormForOwnerLocked(gameID uint32, objectID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil {
		return
	}
	if run := match.abilityWorld.gravityStorms[objectID]; run != nil {
		run.retireLocked()
	}
}

func (e campaignAbilityCommandRuntime) handleArenaGravityStorm(packet raknet.Packet,
	command raknet.ActionCommandData, member gameplayPeerSession, abilityID uint32,
	definition sim.AbilityDefinition,
) ([][]byte, error) {
	if command.Ability == nil || command.Common.Type != raknet.ActionUseCharacterAbility ||
		definition.Name != "GravityStorm" || definition.Kind != sim.AbilityKindChannelArea ||
		definition.Range <= 0 || definition.Radius <= 0 || definition.HitDelay <= 0 ||
		definition.Duration <= 0 || definition.TickDuration <= 0 || definition.FinalWaitDelay <= 0 ||
		definition.MinimumDamage <= 0 || definition.MaximumDamage < definition.MinimumDamage ||
		definition.RootModifierID == 0 || definition.SpawnNoun == "" || definition.ActivationEffectName == "" {
		return e.rejectArenaCharacter(command, "Gravity Storm definition unavailable")
	}
	if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
		return e.rejectArenaCharacter(command, "Gravity Storm schedule unavailable")
	}
	sessionKey := packet.Address.String()
	startedAt := e.now()
	e.registry.mutex.Lock()
	source, isFound := e.registry.sessions[sessionKey]
	match := e.registry.arenaMatches[member.binding.GameID]
	isReady := isFound && source.generation == member.generation && source.binding.Mode == game.ModeArena &&
		source.binding.GameID == member.binding.GameID && source.isArenaCombatActive && source.stage.IsDungeon() &&
		match != nil && match.isCombatActive && match.random != nil && e.registry.timer != nil && source.deployedObjectID == command.Common.ObjectID &&
		source.deployedHitPoint() > 0 && source.isAbilityReleaseReady(startedAt)
	if !isReady {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Gravity Storm source unavailable")
	}
	if source.isEnemyStunActive(startedAt) || source.isEnemySilenceActive(startedAt) {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Gravity Storm source controlled")
	}
	err := source.advancePlayerPosition(startedAt, raknet.Vector3{})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("gravitySourcePosition: %w", err)
	}
	center := command.Ability.TargetPosition
	if !isReportedZonePosition(center) {
		center = command.Ability.CursorPosition
	}
	creature := e.registry.projectPassiveCreature(source, source.deployedCreatureIndex, startedAt)
	if !isReportedZonePosition(center) || !isFiniteZonePosition(center) ||
		!isInsideZoneTrigger(source.playerPosition, center, heroAbilityAdmissionRange(creature, definition)) {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Gravity Storm position unavailable")
	}
	if definition.IsAreaRadiusScaled {
		radius, err := zoneability.ProjectAreaRadius(creature, definition.Radius)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("gravityRadius: %w", err)
		}
		definition.Radius = radius
	}
	world, err := e.registry.arenaAbilityWorldLocked(source.binding.GameID)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("gravityWorld: %w", err)
	}
	if world.gravityStorms[source.deployedObjectID] != nil {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Gravity Storm already active")
	}
	effectObjectID, err := e.registry.reserveAbilityObjectIDsLocked(&source, 1, 0)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("gravityObjectID: %w", err)
	}
	effectSlot, isAllocated := e.effectPool.Allocate(effectObjectID)
	if !isAllocated {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Gravity Storm effect unavailable")
	}
	defer func() {
		if isAllocated {
			e.effectPool.Release(effectObjectID, effectSlot)
		}
	}()
	cooldown, err := zoneability.ProjectCooldown(creature, definition)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("gravityCooldown: %w", err)
	}
	channel, err := zoneability.ProjectDuration(creature, definition, definition.Duration)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("gravityDuration: %w", err)
	}
	slamStart := definition.HitDelay + channel
	slamHit := slamStart + definition.TickDuration
	releaseAt := slamStart + gravityStormSlamDuration
	cleanupAt := releaseAt + definition.FinalWaitDelay
	manaCost, err := game.ResolveAbilityManaCost(definition.ManaCost, creature.DamageProfile.PrimaryAttribute,
		definition.ManaCoefficient, source.isOverdriveActiveAt(startedAt))
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("gravityMana: %w", err)
	}
	if source.deployedManaPoint() < manaCost {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Gravity Storm power unavailable")
	}
	start, err := abilityraknet.StartAreaBasic(abilityraknet.AreaBasicStartRequest{
		SyncStamp: command.Common.Unknown[0], SourceID: command.Common.ObjectID,
		AbilityID: abilityID, AbilityIndex: command.Ability.Index, SourceTime: packet.SourceTime,
		AnimationName: definition.AnimationName, HitDelay: definition.HitDelay,
		ReleaseDelay: releaseAt, Cooldown: cooldown, ChannelDuration: channel,
	})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("gravityStart: %w", err)
	}
	remainingMana := source.deployedManaPoint() - manaCost
	manaPacket, err := abilityraknet.Mana(source.deployedObjectID, remainingMana)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("gravityManaPacket: %w", err)
	}
	createPacket, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
		ObjectID: effectObjectID, Noun: util.HashID(definition.SpawnNoun),
		PositionX: center.X, PositionY: center.Y, PositionZ: center.Z,
		Scale: definition.Radius, Team: abilityActorTeam(source), OwnerID: source.deployedObjectID,
		IsCollisionEnabled: false,
	})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("gravityCreate: %w", err)
	}
	positionPacket, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID: effectObjectID, PositionX: center.X, PositionY: center.Y,
		PositionZ: center.Z, IsVisible: true,
	})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("gravityPosition: %w", err)
	}
	effectPacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: effectSlot + 1, IsForceAttached: true,
		Asset: util.HashID(definition.ActivationEffectName), ObjectID: effectObjectID,
	})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("gravityEffect: %w", err)
	}
	cooldownReservation, isReserved := source.abilityCooldownSession().Reserve(zoneability.HeroAbilityCooldown(abilityID), startedAt, cooldown)
	if !isReserved {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Gravity Storm cooldown unavailable")
	}
	releaseReservation, isReserved := source.abilityReleaseSession().Reserve(startedAt, releaseAt)
	if !isReserved {
		source.abilityCooldownSession().Rollback(cooldownReservation)
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Gravity Storm release unavailable")
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
		return nil, fmt.Errorf("gravityCommit: %w", err)
	}
	run := &arenaGravityStormRun{runtime: e, packet: packet, world: world,
		sessionKey: sessionKey, gameID: source.binding.GameID, generation: source.generation,
		objectID: source.deployedObjectID, effectObjectID: effectObjectID, effectSlot: effectSlot,
		center: center, creature: creature, definition: definition,
		rootDuration: cleanupAt - definition.HitDelay,
		slamStart:    slamStart, slamHit: slamHit, releaseAt: releaseAt, cleanupAt: cleanupAt,
		releasePacket: start.Release, startedAt: startedAt, isEffectBound: true}
	if world.gravityStorms == nil {
		world.gravityStorms = make(map[uint32]*arenaGravityStormRun)
	}
	world.gravityStorms[run.objectID] = run
	e.registry.sessions[sessionKey] = source
	e.registry.mutex.Unlock()
	producers := []raknet.ScheduledPacketProducer{
		{Delay: definition.HitDelay, Produce: run.lift},
		{Delay: definition.HitDelay + gravityStormEffectShutdownDelay, Produce: run.shutdownEffect},
		{Delay: definition.HitDelay + gravityStormEffectShutdownDelay + gravityStormEffectDeleteDelay, Produce: run.deleteEffect},
		{Delay: slamStart, Produce: run.startSlam},
		{Delay: slamHit, Produce: run.slam},
		{Delay: releaseAt, Produce: run.release},
		{Delay: cleanupAt, Produce: run.cleanup},
	}
	producers = e.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(producers, run.fail)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err == nil && cancel == nil {
		err = errors.New("Gravity Storm cancellation unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		e.registry.mutex.Lock()
		latest, isCurrent := run.currentLocked()
		if isCurrent {
			delete(world.gravityStorms, run.objectID)
			latest.abilityCooldownSession().Rollback(cooldownReservation)
			latest.abilityReleaseSession().Rollback(releaseReservation)
			manaErr := latest.setDeployedManaPoints(previousMana)
			if manaErr != nil && e.logger != nil {
				e.logger.Printf("Gravity Storm mana rollback failed: %v", manaErr)
			}
			e.registry.sessions[sessionKey] = latest
		}
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("gravitySchedule: %w", err)
	}
	e.registry.mutex.Lock()
	if world.gravityStorms[run.objectID] != run {
		e.registry.mutex.Unlock()
		cancel()
		isAllocated = false
		return nil, errors.New("Gravity Storm retired during admission")
	}
	run.cancel = cancel
	run.isEffectPublished = true
	e.registry.mutex.Unlock()
	isAllocated = false
	presentation := append(append([][]byte(nil), start.Presentation...), createPacket, positionPacket, effectPacket)
	response := append([][]byte{start.Acknowledge, manaPacket}, presentation...)
	return response, nil
}

func (e *arenaGravityStormRun) lift() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	source, isCurrent := e.currentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	timestamp := e.packet.SourceTime + uint64(e.definition.HitDelay/time.Millisecond)
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, source, e.definition, timestamp).(*arenaAbilityTargets)
	objectIDs := make([]uint32, 0)
	for _, target := range targets.LiveSnapshots() {
		if target.Plan.Position.Sub(game.Vec3(e.center)).Length() <= e.definition.Radius {
			objectIDs = append(objectIDs, target.Plan.ObjectID)
		}
	}
	definition := e.definition
	definition.StatusKind = sim.AbilityStatusKindStun
	definition.StatusDuration = e.rootDuration
	plan := zoneability.AreaPlan{SourceObjectID: e.objectID, AbilityID: util.HashID(definition.Name), Definition: definition}
	_, err := e.runtime.applyArenaTargetStatusLocked(e.sessionKey, e.generation, e.objectID, timestamp, plan, objectIDs)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("gravityLiftStatus: %w", err)
	}
	packets := make([][]byte, 0)
	for _, objectID := range objectIDs {
		_, member, isFound := targets.member(objectID)
		if !isFound {
			continue
		}
		status := member.arenaHitStatuses[arenaHitStatusKey{modifierID: definition.RootModifierID}]
		if status == nil || status.sourceObjectID != e.objectID || status.kind != sim.AbilityStatusKindStun {
			continue
		}
		e.targetIDs = append(e.targetIDs, objectID)
		animation, animationErr := npcraknet.AnimationState(objectID, "gravitystorm_target_lift", timestamp)
		if animationErr != nil {
			e.runtime.registry.mutex.Unlock()
			targets.finish()
			return nil, fmt.Errorf("gravityLiftAnimation: %w", animationErr)
		}
		packets = append(packets, animation)
	}
	e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, packets)
	e.runtime.registry.mutex.Unlock()
	targets.finish()
	return nil, nil
}

func (e *arenaGravityStormRun) shutdownEffect() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	_, isCurrent := e.currentLocked()
	if !isCurrent || !e.isEffectBound {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: e.effectSlot + 1, ObjectID: e.effectObjectID, IsRemovalRequested: true,
	})
	if err != nil {
		return nil, fmt.Errorf("gravityEffectShutdown: %w", err)
	}
	e.runtime.effectPool.Release(e.effectObjectID, e.effectSlot)
	e.isEffectBound = false
	e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, [][]byte{packet})
	return nil, nil
}

func (e *arenaGravityStormRun) deleteEffect() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	_, isCurrent := e.currentLocked()
	if !isCurrent || e.isEffectDeleted {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.effectObjectID}})
	if err != nil {
		return nil, fmt.Errorf("gravityEffectDelete: %w", err)
	}
	e.isEffectDeleted = true
	e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, [][]byte{packet})
	return nil, nil
}

func (e *arenaGravityStormRun) startSlam() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	_, isCurrent := e.currentLocked()
	if !isCurrent {
		return nil, nil
	}
	timestamp := e.packet.SourceTime + uint64(e.slamStart/time.Millisecond)
	packet, err := npcraknet.AnimationState(e.objectID, e.definition.SecondaryAnimationName, timestamp)
	if err != nil {
		return nil, fmt.Errorf("gravitySlamAnimation: %w", err)
	}
	packets := [][]byte{packet}
	for _, objectID := range e.targetIDs {
		animation, animationErr := npcraknet.AnimationState(objectID, "gravitystorm_target_slam", timestamp)
		if animationErr != nil {
			return nil, fmt.Errorf("gravityTargetSlam: %w", animationErr)
		}
		packets = append(packets, animation)
	}
	e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, packets)
	return nil, nil
}

func (e *arenaGravityStormRun) slam() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	source, isCurrent := e.currentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	timestamp := e.packet.SourceTime + uint64(e.slamHit/time.Millisecond)
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, source, e.definition, timestamp).(*arenaAbilityTargets)
	snapshots := make([]zonenpc.Snapshot, 0, len(e.targetIDs))
	for _, objectID := range e.targetIDs {
		target, isFound := targets.NPC(objectID)
		if isFound {
			snapshots = append(snapshots, target)
		}
	}
	damage, err := zoneability.ProjectDamage(e.creature, e.definition, e.definition.MinimumDamage, e.definition.MaximumDamage)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("gravityDamageProject: %w", err)
	}
	plan := zoneability.AreaPlan{SourceObjectID: e.objectID, AbilityID: util.HashID(e.definition.Name),
		Definition: e.definition, Damage: damage, Center: game.Vec3(e.center), Target: snapshots}
	results, err := zoneability.CommitArea(e.runtime.registry.arenaMatches[e.gameID].random,
		targets, plan, e.creature, source.binding.Difficulty, e.runtime.program.Critical)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("gravityDamage: %w", err)
	}
	packets, err := targets.packets(results)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("gravityDamagePublish: %w", err)
	}
	e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, packets)
	e.runtime.registry.mutex.Unlock()
	targets.finish()
	return nil, nil
}

func (e *arenaGravityStormRun) release() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	_, isCurrent := e.currentLocked()
	if !isCurrent {
		return nil, nil
	}
	timestamp := e.packet.SourceTime + uint64(e.releaseAt/time.Millisecond)
	packets := make([][]byte, 0, len(e.targetIDs)+1)
	for _, objectID := range e.targetIDs {
		packet, err := npcraknet.AnimationState(objectID, "gravitystorm_target_getup", timestamp)
		if err != nil {
			return nil, fmt.Errorf("gravityGetUp: %w", err)
		}
		packets = append(packets, packet)
	}
	e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, packets)
	return [][]byte{e.releasePacket}, nil
}

func (e *arenaGravityStormRun) cleanup() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	e.retireLocked()
	return nil, nil
}

func (e *arenaGravityStormRun) fail(cause error) {
	e.runtime.registry.mutex.Lock()
	e.retireLocked()
	e.runtime.registry.mutex.Unlock()
	if e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena Gravity Storm schedule stopped owner=%d: %v", e.objectID, cause)
	}
}
