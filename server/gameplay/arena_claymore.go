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
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
)

// Arena mines live in the match world, so their IDs and expiry remain unique
// across both teams without manufacturing a campaign NPC or companion zone.
type arenaClaymoreRun struct {
	runtime       campaignAbilityCommandRuntime
	packet        raknet.Packet
	world         *arenaAbilityWorld
	sessionKey    string
	gameID        uint32
	generation    uint64
	ownerObjectID uint32
	objectID      uint32
	startedAt     time.Time
	position      raknet.Vector3
	creature      game.GameplayCreature
	definition    sim.AbilityDefinition
	hitPoint      float32
	footprint     float32
	spawnPackets  [][]byte
	releasePacket []byte
	deletePacket  []byte
	cancel        raknet.CancelSchedule
	triggerCancel raknet.CancelSchedule
	isTriggered   bool
	isPublished   bool
}

func (e *arenaClaymoreRun) isCurrentLocked() (gameplayPeerSession, bool) {
	match := e.runtime.registry.arenaMatches[e.gameID]
	source, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := !e.runtime.registry.isClosed && match != nil && match.isCombatActive &&
		match.abilityWorld == e.world && e.world.claymores[e.objectID] == e &&
		isFound && source.generation == e.generation && source.binding.Mode == game.ModeArena &&
		source.binding.GameID == e.gameID && source.isArenaCombatActive && source.stage.IsDungeon()
	return source, isCurrent
}

func (e *arenaClaymoreRun) retireLocked() {
	if e.world.claymores[e.objectID] != e {
		return
	}
	delete(e.world.claymores, e.objectID)
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if e.triggerCancel != nil {
		e.triggerCancel()
		e.triggerCancel = nil
	}
	if e.isPublished {
		e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, [][]byte{e.deletePacket})
	}
}

func (e *gameplaySessionRegistry) retireArenaClaymoresLocked(gameID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil {
		return
	}
	for _, run := range match.abilityWorld.claymores {
		run.retireLocked()
	}
}

func (e *gameplaySessionRegistry) retireArenaClaymoresForOwnerLocked(gameID uint32, ownerObjectID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil {
		return
	}
	for _, run := range match.abilityWorld.claymores {
		if run.ownerObjectID == ownerObjectID {
			run.retireLocked()
		}
	}
}

func (e *arenaClaymoreRun) spawn() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	_, isCurrent := e.isCurrentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	e.isPublished = true
	e.runtime.registry.mutex.Unlock()
	return e.spawnPackets, nil
}

func (e *arenaClaymoreRun) release() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	_, isCurrent := e.isCurrentLocked()
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.releasePacket}, nil
}

func (e *arenaClaymoreRun) trigger() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	source, isCurrent := e.isCurrentLocked()
	if !isCurrent || e.isTriggered {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, source, e.definition, e.packet.SourceTime)
	isTriggered := false
	for _, target := range targets.LiveSnapshots() {
		if game.Vec3(e.position).Sub(target.Plan.Position).Length() < claymoreTriggerRange {
			isTriggered = true
			break
		}
	}
	if !isTriggered {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	e.isTriggered = true
	e.runtime.registry.mutex.Unlock()
	producer := raknet.ScheduledPacketProducer{Delay: e.definition.TickDuration, Produce: e.detonate}
	producers := e.runtime.registry.producerGuard.scheduledProducers(e.sessionKey, []raknet.ScheduledPacketProducer{producer})
	cancel, err := e.packet.Autonomous().ScheduleProducers(producers)
	if err == nil && cancel == nil {
		err = errors.New("Claymore Trap trigger cancellation unavailable")
	}
	if err != nil {
		e.fail(err)
		return nil, fmt.Errorf("claymoreTriggerSchedule: %w", err)
	}
	e.runtime.registry.mutex.Lock()
	_, isCurrent = e.isCurrentLocked()
	if isCurrent {
		e.triggerCancel = cancel
	} else if cancel != nil {
		cancel()
	}
	e.runtime.registry.mutex.Unlock()
	return nil, nil
}

func (e *arenaClaymoreRun) detonate() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	source, isCurrent := e.isCurrentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if e.definition.Name == "TurretTrap" {
		packets := make([][]byte, 0, 2)
		if e.definition.ImpactEffectName != "" {
			effectPacket, err := abilityraknet.CursorAreaImpact(e.definition.ImpactEffectName, e.position)
			if err != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("turretRepack: %w", err)
			}
			packets = append(packets, effectPacket)
		}
		packets = append(packets, e.deletePacket)
		delete(e.world.claymores, e.objectID)
		e.cancel = nil
		e.runtime.registry.mutex.Unlock()
		return packets, nil
	}
	elapsed := max(time.Duration(0), e.runtime.now().Sub(e.startedAt))
	timestamp := e.packet.SourceTime + uint64(elapsed/time.Millisecond)
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, source, e.definition, timestamp).(*arenaAbilityTargets)
	plan, err := zoneability.PlanTrapArea(targets, e.ownerObjectID, game.Vec3(e.position), e.creature, e.definition)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("claymorePlan: %w", err)
	}
	random, err := e.runtime.registry.abilityRandomLocked(source)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("claymoreRandom: %w", err)
	}
	results, err := zoneability.CommitArea(random, targets, plan, e.creature, source.binding.Difficulty, e.runtime.program.Critical)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("claymoreDamage: %w", err)
	}
	targetIDs := make([]uint32, 0, len(results))
	for _, result := range results {
		if !result.Damage.IsDefeated &&
			(result.Damage.Damage > 0 || e.definition.Name == "PipeBomb") {
			targetIDs = append(targetIDs, result.Damage.ObjectID)
		}
	}
	statusSourceID := source.deployedObjectID
	if statusSourceID == e.ownerObjectID {
		_, err = e.runtime.applyArenaTargetStatusLocked(e.sessionKey, e.generation, statusSourceID, timestamp, plan, targetIDs)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			targets.finish()
			return nil, fmt.Errorf("claymoreStatus: %w", err)
		}
	}
	packets, err := targets.packets(results)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("claymorePackets: %w", err)
	}
	if e.definition.Name == "PipeBomb" && e.definition.HitEffectName != "" {
		impact := campaignPointBlankImpact{
			assetName:  e.definition.HitEffectName,
			attackerID: e.ownerObjectID,
			source:     e.position,
		}
		for _, result := range results {
			hitPacket, hitErr := impact.marshal(result)
			if hitErr != nil {
				e.runtime.registry.mutex.Unlock()
				targets.finish()
				return nil, fmt.Errorf("pipeBombHitEffect: %w", hitErr)
			}
			if hitPacket != nil {
				packets = append([][]byte{hitPacket}, packets...)
			}
		}
	}
	if e.definition.ImpactEffectName != "" {
		effectPacket, effectErr := abilityraknet.CursorAreaImpact(e.definition.ImpactEffectName, e.position)
		if effectErr != nil {
			e.runtime.registry.mutex.Unlock()
			targets.finish()
			return nil, fmt.Errorf("claymoreImpact: %w", effectErr)
		}
		packets = append([][]byte{effectPacket}, packets...)
	}
	if e.definition.Name == "PipeBomb" {
		removePacket, removeErr := abilityraknet.TrapRemoveEffect(e.objectID)
		if removeErr != nil {
			e.runtime.registry.mutex.Unlock()
			targets.finish()
			return nil, fmt.Errorf("pipeBombRemove: %w", removeErr)
		}
		packets = append([][]byte{removePacket}, packets...)
	}
	packets = append(packets, e.deletePacket)
	delete(e.world.claymores, e.objectID)
	e.cancel = nil
	e.triggerCancel = nil
	e.runtime.registry.mutex.Unlock()
	targets.finish()
	return packets, nil
}

func (e *arenaClaymoreRun) fail(cause error) {
	e.runtime.registry.mutex.Lock()
	_, isCurrent := e.isCurrentLocked()
	if isCurrent {
		e.retireLocked()
	}
	e.runtime.registry.mutex.Unlock()
	if cause != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena Claymore Trap producer failed game=%d object=%d: %v", e.gameID, e.objectID, cause)
	}
}

func (e campaignAbilityCommandRuntime) handleArenaClaymore(
	packet raknet.Packet, command raknet.ActionCommandData, member gameplayPeerSession,
	abilityID uint32, definition sim.AbilityDefinition,
) ([][]byte, error) {
	isClaymore := definition.Name == "ClaymoreTrap"
	isPipeBomb := definition.Name == "PipeBomb"
	isTurret := definition.Name == "TurretTrap"
	if command.Ability == nil || (!isClaymore && !isPipeBomb && !isTurret) || definition.Kind != sim.AbilityKindTrap ||
		definition.SpawnNoun == "" || definition.Duration <= 0 || (isTurret && definition.Range <= 0) ||
		(!isTurret && definition.TickDuration <= 0) ||
		(isPipeBomb && (!definition.IsForwardPlacement || definition.PlacementDistance <= 0 || definition.ActivationEffectName == "")) ||
		(isClaymore && command.Ability.Index != 3) || (isPipeBomb && command.Ability.Index != 2) ||
		(isTurret && (command.Ability.Index < 6 || command.Ability.Index > 8)) ||
		(!isTurret && command.Common.Type != raknet.ActionUseCharacterAbility) ||
		(isTurret && command.Common.Type != raknet.ActionUseSquadAbility) {
		return e.rejectArenaCharacter(command, "Arena trap definition unavailable")
	}
	if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
		return e.rejectArenaCharacter(command, "Claymore Trap schedule unavailable")
	}
	sessionKey := packet.Address.String()
	startTime := e.now()
	e.registry.mutex.Lock()
	source, isFound := e.registry.sessions[sessionKey]
	match := e.registry.arenaMatches[member.binding.GameID]
	isReady := isFound && source.generation == member.generation && source.binding.Mode == game.ModeArena &&
		source.binding.GameID == member.binding.GameID && source.isArenaCombatActive &&
		match != nil && match.isCombatActive && source.stage.IsDungeon() &&
		source.deployedObjectID == command.Common.ObjectID && source.deployedHitPoint() > 0 &&
		source.isAbilityReleaseReady(startTime)
	if !isReady {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Claymore Trap source unavailable")
	}
	if source.isEnemyStunActive(startTime) || source.isEnemySilenceActive(startTime) {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Claymore Trap source controlled")
	}
	creatureIndex := source.deployedCreatureIndex
	if isTurret {
		creatureIndex = command.Ability.Index - 6
	}
	creature := e.registry.projectPassiveCreature(source, creatureIndex, startTime)
	position := command.Ability.TargetPosition
	if isPipeBomb {
		facing, facingErr := geometryraknet.Forward(command.Common.Orientation)
		if facingErr != nil {
			e.registry.mutex.Unlock()
			return e.rejectArenaCharacter(command, "Pipe Bomb facing unavailable")
		}
		position = raknet.Vector3{
			X: source.playerPosition.X + facing.X*definition.PlacementDistance,
			Y: source.playerPosition.Y + facing.Y*definition.PlacementDistance,
			Z: source.playerPosition.Z + facing.Z*definition.PlacementDistance,
		}
	} else if !isReportedZonePosition(position) {
		position = command.Ability.CursorPosition
	}
	if !isReportedZonePosition(position) || !isFiniteZonePosition(position) ||
		!isInsideZoneTrigger(source.playerPosition, position, heroAbilityAdmissionRange(creature, definition)) {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Claymore Trap position unavailable")
	}
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("claymoreTiming: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(projected.ManaCost, creature.DamageProfile.PrimaryAttribute,
		projected.ManaCoefficient, source.isOverdriveActiveAt(startTime))
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("claymoreMana: %w", err)
	}
	if source.deployedManaPoint() < manaCost || !source.abilityCooldownSession().IsReady(zoneability.HeroAbilityCooldown(abilityID), startTime) {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Claymore Trap resource unavailable")
	}
	world, err := e.registry.arenaAbilityWorldLocked(source.binding.GameID)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("claymoreWorld: %w", err)
	}
	objectID, err := world.objectIDSession.Reserve(1)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("claymoreID: %w", err)
	}
	deletePacket, err := abilityraknet.TrapDelete(objectID)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("claymoreDelete: %w", err)
	}
	spawnPacket, err := abilityraknet.TrapSpawnForTeam(
		objectID, command.Common.ObjectID, position, definition, uint8(source.binding.Team),
	)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("claymoreSpawn: %w", err)
	}
	spawnPackets := [][]byte{spawnPacket}
	if isTurret && projected.MuzzleEffectName != "" {
		muzzlePacket, muzzleErr := abilityraknet.TrapObjectEffect(
			projected.MuzzleEffectName, command.Common.ObjectID, command.Common.ObjectID,
		)
		if muzzleErr != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("turretMuzzle: %w", muzzleErr)
		}
		spawnPackets = append([][]byte{muzzlePacket}, spawnPackets...)
	}
	hitPoint := float32(0)
	footprint := float32(0)
	if isTurret {
		hitPoint = e.program.NonPlayerHitPoint[util.HashID(projected.Name)]
		if hitPoint <= 0 {
			hitPoint = turretFallbackHitPoint
		}
		footprint, err = e.program.FootprintRadius(projected.SpawnNoun)
		if err != nil || footprint <= 0 {
			footprint = turretFallbackFootprint
		}
	}
	if isPipeBomb {
		activationPacket, activationErr := abilityraknet.TrapObjectEffect(
			definition.ActivationEffectName, objectID, command.Common.ObjectID,
		)
		if activationErr != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("pipeBombActivation: %w", activationErr)
		}
		tauntPacket, tauntErr := abilityraknet.PipeBombTauntEffect(objectID)
		if tauntErr != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("pipeBombTaunt: %w", tauntErr)
		}
		spawnPackets = append(spawnPackets, activationPacket, tauntPacket)
	}
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: abilityID, AbilityIndex: command.Ability.Index, SourceStartMilliseconds: packet.SourceTime,
		SourceCommitMilliseconds: packet.SourceTime + uint64(projected.HitDelay/time.Millisecond),
		SourceEndMilliseconds:    packet.SourceTime + uint64(projected.ReleaseDelay/time.Millisecond),
	})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("claymoreAck: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(command.Common.Unknown[0], abilityID,
		command.Ability.Index, packet.SourceTime, projected.HitDelay, projected.ReleaseDelay)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("claymoreRelease: %w", err)
	}
	remainingMana := source.deployedManaPoint() - manaCost
	startPackets, err := abilityraknet.StartSpendPresentation(command.Common.ObjectID, abilityID,
		projected.AnimationName, projected.Cooldown, packet.SourceTime, remainingMana)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("claymoreStart: %w", err)
	}
	stopPackets, err := marshalZonePlayerStop(command.Common.ObjectID, source.playerPosition)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("claymoreStop: %w", err)
	}
	cooldownReservation, isReserved := source.abilityCooldownSession().Reserve(zoneability.HeroAbilityCooldown(abilityID), startTime, projected.Cooldown)
	if !isReserved {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Claymore Trap cooldown unavailable")
	}
	releaseReservation, isReserved := source.abilityReleaseSession().Reserve(startTime, projected.ReleaseDelay)
	if !isReserved {
		source.abilityCooldownSession().Rollback(cooldownReservation)
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Claymore Trap release unavailable")
	}
	previousMana := source.deployedManaPoint()
	err = source.stopPlayerMovement(startTime)
	if err == nil {
		err = source.setDeployedManaPoints(remainingMana)
	}
	if err != nil {
		source.abilityCooldownSession().Rollback(cooldownReservation)
		source.abilityReleaseSession().Rollback(releaseReservation)
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("claymoreCommit: %w", err)
	}
	run := &arenaClaymoreRun{runtime: e, packet: packet, world: world, sessionKey: sessionKey,
		gameID: source.binding.GameID, generation: source.generation, ownerObjectID: command.Common.ObjectID,
		objectID: objectID, startedAt: startTime, position: position, creature: creature, definition: projected,
		hitPoint: hitPoint, footprint: footprint,
		spawnPackets: spawnPackets, releasePacket: releasePacket, deletePacket: deletePacket}
	if world.claymores == nil {
		world.claymores = make(map[uint32]*arenaClaymoreRun)
	}
	world.claymores[objectID] = run
	e.registry.sessions[sessionKey] = source
	e.registry.mutex.Unlock()
	producers := []raknet.ScheduledPacketProducer{
		{Delay: projected.HitDelay, Produce: run.spawn},
		{Delay: projected.ReleaseDelay, Produce: run.release},
	}
	timeout := projected.Duration + projected.TickDuration
	if isTurret {
		timeout = projected.Duration
		for delay := projected.HitDelay + time.Second; delay < timeout; delay += time.Second {
			fire := &arenaTurretStep{run: run, deadline: delay}
			producers = append(producers, raknet.ScheduledPacketProducer{Delay: delay, Produce: fire.produce})
		}
	}
	if isClaymore {
		timeout += projected.HitDelay + claymoreArmDelay
		for delay := projected.HitDelay + claymoreArmDelay; delay < timeout-projected.TickDuration; delay += claymoreTriggerInterval {
			producers = append(producers, raknet.ScheduledPacketProducer{Delay: delay, Produce: run.trigger})
		}
	}
	producers = append(producers, raknet.ScheduledPacketProducer{Delay: timeout, Produce: run.detonate})
	producers = e.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(producers, run.fail)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err == nil && cancel == nil {
		err = errors.New("Claymore Trap cancellation unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		e.registry.mutex.Lock()
		latest, isCurrent := run.isCurrentLocked()
		if isCurrent {
			delete(world.claymores, objectID)
			latest.abilityCooldownSession().Rollback(cooldownReservation)
			latest.abilityReleaseSession().Rollback(releaseReservation)
			manaErr := latest.setDeployedManaPoints(previousMana)
			if manaErr != nil && e.logger != nil {
				e.logger.Printf("Arena Claymore Trap payment rollback failed: %v", manaErr)
			}
			e.registry.sessions[sessionKey] = latest
		}
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("claymoreSchedule: %w", err)
	}
	run.cancel = cancel
	response := append([][]byte{ackPacket}, startPackets...)
	response = append(response, stopPackets...)
	return response, nil
}
