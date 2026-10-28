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
)

type arenaSproutRun struct {
	runtime            campaignAbilityCommandRuntime
	packet             raknet.Packet
	world              *arenaSproutWorld
	sessionKey         string
	gameID             uint32
	generation         uint64
	sourceObjectID     uint32
	projectileObjectID uint32
	cloudObjectID      uint32
	projectileIndex    int
	plan               zoneability.CloudLobPlan
	creature           game.GameplayCreature
	source             gameplayPeerSession
	startedAt          time.Time
	landedAt           time.Time
	expiresAt          time.Time
	releasePacket      []byte
	cancel             raknet.CancelSchedule
	isLaunched         bool
	isLanded           bool
	occupants          map[uint32]struct{}
}

func (e *arenaSproutRun) currentLocked() (gameplayPeerSession, bool) {
	source, isFound := e.runtime.registry.sessions[e.sessionKey]
	match := e.runtime.registry.arenaMatches[e.gameID]
	isCurrent := isFound && !e.runtime.registry.isClosed && match != nil && match.isCombatActive &&
		match.abilityWorld != nil && match.abilityWorld.sproutWorld == e.world &&
		e.world.runs[e.cloudObjectID] == e && source.generation == e.generation &&
		source.binding.Mode == game.ModeArena && source.binding.GameID == e.gameID &&
		source.isArenaCombatActive && source.deployedObjectID == e.sourceObjectID &&
		source.deployedHitPoint() > 0
	return source, isCurrent
}

func (e campaignAbilityCommandRuntime) handleArenaSprout(packet raknet.Packet,
	command raknet.ActionCommandData, member gameplayPeerSession,
	definition sim.AbilityDefinition,
) ([][]byte, error) {
	if command.Ability == nil || command.Common.Type != raknet.ActionUseCharacterAbility ||
		definition.Name != "Sprout" || definition.Kind != sim.AbilityKindCloudLob {
		return e.rejectArenaCharacter(command, "Sprout definition unavailable")
	}
	if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
		return e.rejectArenaCharacter(command, "Sprout schedule unavailable")
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
		return e.rejectArenaCharacter(command, "Sprout source unavailable")
	}
	if source.isEnemyStunActive(startedAt) || source.isEnemySilenceActive(startedAt) {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Sprout source controlled")
	}
	err := source.advancePlayerPosition(startedAt, raknet.Vector3{})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("sproutSourcePosition: %w", err)
	}
	creature := e.registry.projectPassiveCreature(source, source.deployedCreatureIndex, startedAt)
	cloudReq, err := campaignCloudLobAim(source, creature, definition, command)
	if err != nil {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Sprout aim unavailable")
	}
	plan, err := zoneability.PlanCloudLobAt(cloudReq)
	if err != nil {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Sprout target unavailable")
	}
	previousSequence := source.basicSequenceSession().Snapshot()
	selection, err := source.basicSequenceSession().Accept(startedAt, plan.Definition)
	if err != nil {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Sprout basic sequence unavailable")
	}
	sequenceRevision := source.basicSequenceSession().Revision()
	plan.Definition.AnimationName = selection.AnimationName
	plan.Definition.HitDelay = selection.HitDelay
	plan.Definition.ReleaseDelay = selection.ReleaseDelay
	objectID, err := e.registry.reserveAbilityObjectIDsLocked(&source, 2, 1000)
	if err != nil {
		source.basicSequenceSession().Restore(previousSequence, sequenceRevision)
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("sproutObjectID: %w", err)
	}
	cloudObjectID := objectID + 1
	index := selection.AnimationIndex % len(plan.Lob)
	landingDelay := selection.HitDelay + plan.Lob[index].Duration
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: plan.AbilityID, AbilityIndex: command.Ability.Index,
		SourceStartMilliseconds:  packet.SourceTime,
		SourceCommitMilliseconds: packet.SourceTime + uint64(selection.HitDelay/time.Millisecond),
		SourceEndMilliseconds:    packet.SourceTime + uint64(selection.ReleaseDelay/time.Millisecond),
	})
	if err != nil {
		source.basicSequenceSession().Restore(previousSequence, sequenceRevision)
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("sproutAck: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(command.Common.Unknown[0], plan.AbilityID,
		command.Ability.Index, packet.SourceTime, selection.HitDelay, selection.ReleaseDelay)
	if err != nil {
		source.basicSequenceSession().Restore(previousSequence, sequenceRevision)
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("sproutRelease: %w", err)
	}
	startPackets, err := abilityraknet.StartPresentation(command.Common.ObjectID, plan.AbilityID,
		selection.AnimationName, plan.Definition.Cooldown, packet.SourceTime, 0)
	if err != nil {
		source.basicSequenceSession().Restore(previousSequence, sequenceRevision)
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("sproutStart: %w", err)
	}
	releaseReservation, isReserved := source.abilityReleaseSession().Reserve(startedAt, selection.ReleaseDelay)
	if !isReserved {
		source.basicSequenceSession().Restore(previousSequence, sequenceRevision)
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Sprout release unavailable")
	}
	previousMotion := source.playerMotionSnapshot()
	err = source.stopPlayerMovement(startedAt)
	if err != nil {
		source.basicSequenceSession().Restore(previousSequence, sequenceRevision)
		source.abilityReleaseSession().Rollback(releaseReservation)
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("sproutStop: %w", err)
	}
	world, err := e.registry.arenaSproutWorldLocked(source.binding.GameID)
	if err != nil {
		source.basicSequenceSession().Restore(previousSequence, sequenceRevision)
		source.abilityReleaseSession().Rollback(releaseReservation)
		source.restorePlayerMotion(previousMotion, source.playerMotionRevision())
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("sproutWorld: %w", err)
	}
	run := &arenaSproutRun{runtime: e, packet: packet, world: world,
		sessionKey: sessionKey, gameID: source.binding.GameID, generation: source.generation,
		sourceObjectID: source.deployedObjectID, projectileObjectID: objectID, cloudObjectID: cloudObjectID,
		projectileIndex: index, plan: plan, creature: creature, source: source, startedAt: startedAt,
		releasePacket: releasePacket, occupants: make(map[uint32]struct{})}
	world.runs[cloudObjectID] = run
	e.registry.sessions[sessionKey] = source
	e.registry.mutex.Unlock()
	producers := []raknet.ScheduledPacketProducer{
		{Delay: selection.HitDelay, Produce: run.launch},
		{Delay: landingDelay, Produce: run.land},
		{Delay: selection.ReleaseDelay, Produce: run.release},
	}
	producers = e.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(producers, run.fail)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err == nil && cancel == nil {
		err = errors.New("Sprout cancellation unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		e.registry.mutex.Lock()
		latest, isCurrent := run.currentLocked()
		if isCurrent {
			delete(world.runs, run.cloudObjectID)
			latest.basicSequenceSession().Restore(previousSequence, sequenceRevision)
			latest.abilityReleaseSession().Rollback(releaseReservation)
			latest.restorePlayerMotion(previousMotion, latest.playerMotionRevision())
			e.registry.sessions[sessionKey] = latest
		}
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("sproutSchedule: %w", err)
	}
	e.registry.mutex.Lock()
	if world.runs[run.cloudObjectID] != run {
		e.registry.mutex.Unlock()
		cancel()
		return nil, errors.New("Sprout retired during admission")
	}
	run.cancel = cancel
	e.registry.mutex.Unlock()
	return append([][]byte{ackPacket}, startPackets...), nil
}

func (e *arenaSproutRun) launch() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	_, isCurrent := e.currentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packets, err := abilityraknet.CloudLobLaunchForTeam(e.plan, e.projectileObjectID, e.projectileIndex, e.packet.SourceTime, abilityActorTeam(e.source))
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("sproutLaunch: %w", err)
	}
	e.isLaunched = true
	e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, packets)
	e.runtime.registry.mutex.Unlock()
	return nil, nil
}

func (e *arenaSproutRun) land() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	_, isCurrent := e.currentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packets, err := abilityraknet.CloudLobLandingForTeam(e.plan, e.projectileObjectID, e.cloudObjectID, abilityActorTeam(e.source))
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("sproutLanding: %w", err)
	}
	e.isLaunched = false
	e.isLanded = true
	e.landedAt = e.runtime.now()
	e.expiresAt = e.landedAt.Add(e.plan.Definition.CloudLob.CloudDuration)
	e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, packets)
	resolveErr := e.world.resolveLocked(e.runtime.now())
	scheduleErr := e.world.scheduleLocked()
	e.runtime.registry.mutex.Unlock()
	if resolveErr != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena Sprout accepted landing poison failed cloud=%d: %v", e.cloudObjectID, resolveErr)
	}
	if scheduleErr != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena Sprout accepted landing poll failed cloud=%d: %v", e.cloudObjectID, scheduleErr)
	}
	return nil, nil
}

func (e *arenaSproutRun) release() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	_, isCurrent := e.currentLocked()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.releasePacket}, nil
}

func (e *arenaSproutRun) fail(cause error) {
	e.runtime.registry.mutex.Lock()
	e.retireLocked()
	e.runtime.registry.mutex.Unlock()
	if e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena Sprout schedule stopped cloud=%d: %v", e.cloudObjectID, cause)
	}
}

func (e *arenaSproutRun) retireLocked() {
	if e.world.runs[e.cloudObjectID] != e {
		return
	}
	delete(e.world.runs, e.cloudObjectID)
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	objectID := uint32(0)
	if e.isLanded {
		objectID = e.cloudObjectID
	} else if e.isLaunched {
		objectID = e.projectileObjectID
	}
	if objectID != 0 {
		packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{objectID}})
		if err != nil {
			if e.runtime.logger != nil {
				e.runtime.logger.Printf("Arena Sprout object cleanup failed object=%d: %v", objectID, err)
			}
		} else {
			e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, [][]byte{packet})
		}
	}
	for _, target := range e.world.targets {
		if target.origin == e && !e.isLanded {
			e.world.retireTargetLocked(target)
		}
	}
}
