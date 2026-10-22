package gameplay

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const gravityStormSlamDuration = 1200 * time.Millisecond
const gravityStormEffectShutdownDelay = 75 * time.Millisecond
const gravityStormEffectDeleteDelay = 2 * time.Second

type heroChannelAreaTarget struct {
	snapshot    zonenpc.Snapshot
	instanceID  uint32
	expiresAt   time.Time
	isPublished bool
}

type heroChannelAreaRun struct {
	mutex                 sync.Mutex
	npc                   *zonenpc.Session
	modifierPool          *modifierPool
	targets               []heroChannelAreaTarget
	effectPool            *attachedEffectPool
	effectObjectID        uint32
	effectSlot            uint8
	isEffectBound         bool
	isEffectObjectDeleted bool
	cancel                raknet.CancelSchedule
	isCleaned             bool
	isEffectPublished     bool
	isAdmitted            bool
	isGettingUp           bool
	sourceTime            uint64
	startedAt             time.Time
	registry              *gameplaySessionRegistry
	zone                  *zone.Zone
	timer                 zone.Timer
	cast                  *channelCast
}

func (e *heroChannelAreaRun) SetTargets(targets []heroChannelAreaTarget) bool {
	if e == nil {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isCleaned {
		return false
	}
	e.targets = targets
	return true
}

func (e *heroChannelAreaRun) Targets() []heroChannelAreaTarget {
	if e == nil {
		return nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return append([]heroChannelAreaTarget(nil), e.targets...)
}

func (e *heroChannelAreaRun) ReleaseTargets(targets []heroChannelAreaTarget) {
	if e == nil {
		return
	}
	for _, target := range targets {
		e.npc.ClearStun(target.snapshot.Plan.ObjectID, target.expiresAt)
		err := e.modifierPool.Release(target.instanceID)
		if err != nil {
			log.Printf("RakNet channel modifier release failed instance=%d: %v", target.instanceID, err)
		}
	}
}

func (e *heroChannelAreaRun) Stop() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	isCleaned := e.isCleaned
	e.mutex.Unlock()
	if isCleaned {
		return
	}
	retirement := channelRetirement{registry: e.registry, area: e}
	retirement.schedule(e.timer)
}

func (e *heroChannelAreaRun) setCancel(cancel raknet.CancelSchedule) {
	e.mutex.Lock()
	if e.isCleaned {
		e.mutex.Unlock()
		if cancel != nil {
			cancel()
		}
		return
	}
	e.cancel = cancel
	e.mutex.Unlock()
}

type heroChannelAreaSchedule struct {
	runtime        campaignAbilityCommandRuntime
	packet         raknet.Packet
	sessionKey     string
	generation     uint64
	sourceObjectID uint32
	center         raknet.Vector3
	creature       game.GameplayCreature
	definition     sim.AbilityDefinition
	binding        game.GameplayBinding
	run            *heroChannelAreaRun
	slamStart      time.Duration
	slamHit        time.Duration
	cleanupAt      time.Duration
	releasePacket  []byte
}

func (e heroChannelAreaSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	e.run.mutex.Lock()
	isActive := e.run.isAdmitted && !e.run.isCleaned
	e.run.mutex.Unlock()
	return isActive && isFound && peerSession.generation == e.generation &&
		peerSession.deployedObjectID == e.sourceObjectID &&
		peerSession.heroChannelArea == e.run
}

func (e heroChannelAreaSchedule) lift() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	e.run.cast.activateLocked(e.run.cast.revision)
	targets := make([]heroChannelAreaTarget, 0)
	messages := make([]raknet.ApplicationMessage, 0)
	for index, target := range peerSession.zone.NPCs().LiveSnapshots() {
		if target.Faction != zonenpc.FactionNonPlayerAligned ||
			target.Plan.IsFixture ||
			zonegeometry.Distance(game.Vec3(e.center), target.Plan.Position) >
				e.definition.Radius {
			continue
		}
		duration := zonenpc.CrowdControlDuration(
			target.Plan, e.cleanupAt-e.definition.HitDelay,
		)
		expiresAt := e.runtime.now().Add(duration)
		instance, err := e.runtime.modifierPool.Allocate()
		if err != nil {
			e.run.ReleaseTargets(targets)
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("channelAreaModifier[%d]: %w", index, err)
		}
		err = peerSession.zone.NPCs().ApplyStun(target.Plan.ObjectID, expiresAt)
		if err != nil {
			releaseErr := e.runtime.modifierPool.Release(instance)
			if releaseErr != nil {
				e.runtime.logger.Printf("RakNet channel modifier allocation cleanup failed instance=%d: %v", instance, releaseErr)
			}
			e.run.ReleaseTargets(targets)
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("channelAreaStun[%d]: %w", index, err)
		}
		if peerSession.zone.NPCs().StunRemaining(target.Plan.ObjectID, e.runtime.now()) == 0 {
			releaseErr := e.runtime.modifierPool.Release(instance)
			if releaseErr != nil {
				e.runtime.logger.Printf("RakNet blocked gravity lift modifier release failed: %v", releaseErr)
			}
			continue
		}
		targets = append(targets, heroChannelAreaTarget{
			snapshot: target, instanceID: instance, expiresAt: expiresAt,
		})
		messages = append(messages, raknet.ModifierCreatedMessage{
			TargetID: target.Plan.ObjectID, ModifierGUID: e.definition.RootModifierID,
			InstanceID: instance, StackCount: 1,
			DurationMilliseconds: uint32(duration.Milliseconds()),
			StartMilliseconds: e.packet.SourceTime +
				uint64(e.definition.HitDelay/time.Millisecond),
			SourceID: e.sourceObjectID,
		})
	}
	if !e.run.SetTargets(targets) {
		e.run.ReleaseTargets(targets)
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("channelAreaLiftMarshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	for _, target := range targets {
		stopPackets, err := npcraknet.StopAtPose(target.snapshot.Plan.ObjectID, target.snapshot.Plan.Position, target.snapshot.Facing)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("gravityLiftStop: %w", err)
		}
		animationPacket, err := npcraknet.AnimationState(target.snapshot.Plan.ObjectID,
			"gravitystorm_target_lift", e.packet.SourceTime+uint64(e.definition.HitDelay/time.Millisecond))
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("gravityLiftAnimation: %w", err)
		}
		packets = append(packets, stopPackets...)
		packets = append(packets, animationPacket)
	}
	e.run.mutex.Lock()
	for index := range e.run.targets {
		e.run.targets[index].isPublished = true
	}
	e.run.mutex.Unlock()
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.queueChannelPresentationLocked(e.run.zone, packets)
	e.runtime.registry.mutex.Unlock()
	return nil, nil
}

func (e heroChannelAreaSchedule) startSlam() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if !isCurrent {
		return nil, nil
	}
	packet, err := npcraknet.AnimationState(
		e.sourceObjectID, e.definition.SecondaryAnimationName,
		e.packet.SourceTime+uint64(e.slamStart/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("channelAreaSlamAnimation: %w", err)
	}
	e.runtime.registry.queueChannelPresentationLocked(e.run.zone, [][]byte{packet})
	err = e.animateTargetsLocked("gravitystorm_target_slam", e.slamStart)
	if err != nil {
		return nil, fmt.Errorf("gravitySlamTargets: %w", err)
	}
	return nil, nil
}

func (e heroChannelAreaSchedule) shutdownEffect() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if !isCurrent {
		return nil, nil
	}
	e.run.mutex.Lock()
	defer e.run.mutex.Unlock()
	if !e.run.isEffectBound {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: e.run.effectSlot + 1, IsRemovalRequested: true,
		ObjectID: e.run.effectObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("channelAreaEffectShutdown: %w", err)
	}
	if e.run.isEffectPublished {
		e.runtime.registry.queueChannelRemovalsLocked(e.run.zone, [][]byte{packet})
	}
	e.run.effectPool.Release(e.run.effectObjectID, e.run.effectSlot)
	e.run.isEffectBound = false
	return nil, nil
}

func (e heroChannelAreaSchedule) deleteEffectObject() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if !isCurrent {
		return nil, nil
	}
	e.run.mutex.Lock()
	defer e.run.mutex.Unlock()
	if e.run.isEffectObjectDeleted {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
		ObjectID: []uint32{e.run.effectObjectID},
	})
	if err != nil {
		return nil, fmt.Errorf("channelAreaEffectDelete: %w", err)
	}
	if e.run.isEffectPublished {
		e.runtime.registry.queueChannelRemovalsLocked(e.run.zone, [][]byte{packet})
	}
	e.run.isEffectObjectDeleted = true
	return nil, nil
}

func (e heroChannelAreaSchedule) slam() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	targets := e.run.Targets()
	e.run.cast.activateLocked(e.run.cast.revision)
	damage, err := zoneability.ProjectDamage(
		e.creature, e.definition,
		e.definition.MinimumDamage, e.definition.MaximumDamage,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("channelAreaDamageProject: %w", err)
	}
	snapshots := make([]zonenpc.Snapshot, 0, len(targets))
	for _, target := range targets {
		snapshots = append(snapshots, target.snapshot)
	}
	results, err := zoneability.CommitArea(
		peerSession.zone.Population().Random(), peerSession.zone.NPCs(),
		zoneability.AreaPlan{
			SourceObjectID: e.sourceObjectID,
			AbilityID:      util.HashID(e.definition.Name), Definition: e.definition,
			Damage: damage, Center: game.Vec3(e.center), Target: snapshots,
		}, e.creature, peerSession.binding.Difficulty,
		e.runtime.program.Critical,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("channelAreaDamage: %w", err)
	}
	transitions := make([]campaignDamageTransition, 0, len(results))
	for index, result := range results {
		transition, transitionErr := peerSession.applyCampaignDamageTransition(result.Damage)
		if transitionErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("channelAreaTransition[%d]: %w", index, transitionErr)
		}
		transitions = append(transitions, transition)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	packets, err := e.runtime.damage.publishAreaResults(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID,
		e.packet.SourceTime+uint64(e.slamHit/time.Millisecond), e.binding,
		results, transitions, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("channelAreaPublish: %w", err)
	}
	return packets, nil
}

func (e heroChannelAreaSchedule) release() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if !isCurrent {
		return nil, nil
	}
	e.runtime.registry.queueChannelReleaseLocked(e.run.zone, e.sessionKey, e.generation, [][]byte{e.releasePacket})
	return nil, nil
}

func (e heroChannelAreaSchedule) cleanup() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if e.isCurrent(peerSession, isFound) {
		peerSession.heroChannelArea = nil
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	err := e.run.retireLocked()
	if err != nil {
		return nil, fmt.Errorf("channelAreaRetire: %w", err)
	}
	return nil, nil
}

func (e heroChannelAreaSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	// Pointer identity decides which action slot to clear; the exact cast's
	// retained phase decides whether its own admission debit can be refunded.
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.deployedObjectID == e.sourceObjectID && peerSession.heroChannelArea == e.run
	if isCurrent {
		peerSession.heroChannelArea = nil
	}
	refundPacket, refundErr := e.run.cast.failLocked(&peerSession, isFound, e.run.cast.revision)
	if isFound {
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	if len(refundPacket) != 0 {
		e.runtime.registry.queueChannelPresentationLocked(e.run.zone, [][]byte{refundPacket})
	}
	if refundErr != nil {
		e.runtime.logger.Printf("RakNet channel area refund failed: %v", refundErr)
	}
	retireErr := e.run.retireLocked()
	e.runtime.registry.mutex.Unlock()
	if retireErr != nil {
		e.runtime.logger.Printf("RakNet area failure retirement failed: %v", retireErr)
	}
	if !isCurrent {
		return
	}
	e.runtime.logger.Printf(
		"RakNet hero channel area stopped after schedule failure for %s: %v",
		e.sessionKey, scheduleErr,
	)
}

func (r campaignAbilityCommandRuntime) handleHeroChannelArea(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	activeAbilityID uint32, sessionKey string, abilityStartTime time.Time,
) ([][]byte, error) {
	if activeAbilityID == 0 || definition.Kind != sim.AbilityKindChannelArea ||
		definition.Name != "GravityStorm" || definition.Range <= 0 ||
		definition.Radius <= 0 || definition.HitDelay <= 0 ||
		definition.Duration <= 0 || definition.TickDuration <= 0 ||
		definition.FinalWaitDelay <= 0 || definition.MinimumDamage <= 0 ||
		definition.MaximumDamage < definition.MinimumDamage ||
		definition.AnimationName == "" || definition.SecondaryAnimationName == "" ||
		definition.SpawnNoun == "" || definition.ActivationEffectName == "" ||
		definition.RootModifierID == 0 {
		r.registry.mutex.Unlock()
		return req.reject("channel area definition unavailable")
	}
	center := req.command.Ability.TargetPosition
	if !isReportedZonePosition(center) {
		center = req.command.Ability.CursorPosition
	}
	admissionRange := heroAbilityAdmissionRange(creature, definition)
	if !isReportedZonePosition(center) || !isFiniteZonePosition(center) ||
		!isInsideZoneTrigger(peerSession.playerPosition, center, admissionRange) {
		r.registry.mutex.Unlock()
		return req.reject("channel area position unavailable")
	}
	if peerSession.heroChannelArea != nil {
		r.registry.mutex.Unlock()
		return req.reject("channel area already active")
	}
	if definition.IsAreaRadiusScaled {
		radius, radiusErr := zoneability.ProjectAreaRadius(creature, definition.Radius)
		if radiusErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("channelAreaRadius: %w", radiusErr)
		}
		definition.Radius = radius
	}
	effectObjectID, err := peerSession.reserveCampaignObjectID()
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("channelAreaEffectObject: %w", err)
	}
	effectSlot, isEffectAllocated := r.effectPool.Allocate(effectObjectID)
	if !isEffectAllocated {
		r.registry.mutex.Unlock()
		return req.reject("channel area effect unavailable")
	}
	createPacket, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
		ObjectID: effectObjectID, Noun: util.HashID(definition.SpawnNoun),
		PositionX: center.X, PositionY: center.Y, PositionZ: center.Z,
		Scale: definition.Radius, Team: 1, OwnerID: req.command.Common.ObjectID,
		IsCollisionEnabled: false,
	})
	if err != nil {
		r.effectPool.Release(effectObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("channelAreaEffectCreate: %w", err)
	}
	positionPacket, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID: effectObjectID, PositionX: center.X, PositionY: center.Y,
		PositionZ: center.Z, IsVisible: true,
	})
	if err != nil {
		r.effectPool.Release(effectObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("gravityEffectPosition: %w", err)
	}
	effectPacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: effectSlot + 1, IsForceAttached: true,
		Asset: util.HashID(definition.ActivationEffectName), ObjectID: effectObjectID,
	})
	if err != nil {
		r.effectPool.Release(effectObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("channelAreaEffectAttach: %w", err)
	}
	cooldown, err := zoneability.ProjectCooldown(creature, definition)
	if err != nil {
		r.effectPool.Release(effectObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("channelAreaCooldown: %w", err)
	}
	timeToSlam, err := zoneability.ProjectDuration(creature, definition, definition.Duration)
	if err != nil {
		r.effectPool.Release(effectObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("channelAreaDuration: %w", err)
	}
	slamStart := definition.HitDelay + timeToSlam
	slamHit := slamStart + definition.TickDuration
	releaseAt := slamStart + gravityStormSlamDuration
	cleanupAt := releaseAt + definition.FinalWaitDelay
	manaCost, err := game.ResolveAbilityManaCost(
		definition.ManaCost, creature.DamageProfile.PrimaryAttribute, definition.ManaCoefficient,
		peerSession.isOverdriveActiveAt(abilityStartTime),
	)
	if err != nil {
		r.effectPool.Release(effectObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("channelAreaMana: %w", err)
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.effectPool.Release(effectObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}
	start, err := abilityraknet.StartAreaBasic(abilityraknet.AreaBasicStartRequest{
		SyncStamp: req.command.Common.Unknown[0], SourceID: req.command.Common.ObjectID,
		AbilityID: activeAbilityID, AbilityIndex: req.command.Ability.Index,
		SourceTime: req.packet.SourceTime, AnimationName: definition.AnimationName,
		HitDelay: definition.HitDelay, ReleaseDelay: releaseAt, Cooldown: cooldown,
		ChannelDuration: timeToSlam,
	})
	if err != nil {
		r.effectPool.Release(effectObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("channelAreaStart: %w", err)
	}
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	manaPacket, err := abilityraknet.Mana(
		req.command.Common.ObjectID, remainingManaPoint,
	)
	if err != nil {
		r.effectPool.Release(effectObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("channelAreaManaPacket: %w", err)
	}
	cooldownReservation, isCooldownReserved :=
		peerSession.abilityCooldownSession().Reserve(
			zoneability.HeroAbilityCooldown(activeAbilityID), abilityStartTime, cooldown,
		)
	if !isCooldownReserved {
		r.effectPool.Release(effectObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return req.reject("channel area cooldown unavailable")
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(abilityStartTime, releaseAt)
	if !isReleaseReserved {
		r.effectPool.Release(effectObjectID, effectSlot)
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		r.registry.mutex.Unlock()
		return req.reject("channel area release unavailable")
	}
	cast := r.registry.reserveChannelCastLocked(peerSession, req.command.Common.ObjectID, cooldownReservation, releaseReservation)
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil {
		err = cast.debitLocked(&peerSession, manaCost)
	}
	if err != nil {
		r.effectPool.Release(effectObjectID, effectSlot)
		refundPacket, refundErr := cast.failLocked(&peerSession, true, cast.revision)
		r.registry.sessions[sessionKey] = peerSession
		if len(refundPacket) != 0 {
			r.registry.queueChannelPresentationLocked(peerSession.zone, [][]byte{refundPacket})
		}
		if refundErr != nil {
			r.logger.Printf("RakNet channel area admission refund failed: %v", refundErr)
		}
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("channelAreaCommit: %w", err)
	}
	run := &heroChannelAreaRun{
		cast:       cast,
		sourceTime: req.packet.SourceTime, startedAt: abilityStartTime,
		registry: r.registry, zone: peerSession.zone, timer: r.npc.timer,
		npc: peerSession.zone.NPCs(), modifierPool: r.modifierPool,
		effectPool: r.effectPool, effectObjectID: effectObjectID,
		effectSlot: effectSlot, isEffectBound: true,
	}
	peerSession.heroChannelArea = run
	generation := peerSession.generation
	binding := peerSession.binding
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	schedule := heroChannelAreaSchedule{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: req.command.Common.ObjectID,
		center: center, creature: creature, definition: definition, binding: binding,
		run: run, slamStart: slamStart, slamHit: slamHit, cleanupAt: cleanupAt,
		releasePacket: start.Release,
	}
	producers := r.registry.producerGuard.scheduledProducers(
		sessionKey, []raknet.ScheduledPacketProducer{
			{Delay: definition.HitDelay, Produce: schedule.lift},
			{Delay: definition.HitDelay + gravityStormEffectShutdownDelay,
				Produce: schedule.shutdownEffect},
			{Delay: definition.HitDelay + gravityStormEffectShutdownDelay +
				gravityStormEffectDeleteDelay, Produce: schedule.deleteEffectObject},
			{Delay: slamStart, Produce: schedule.startSlam},
			{Delay: slamHit, Produce: schedule.slam},
			{Delay: releaseAt, Produce: schedule.getUp},
			{Delay: releaseAt, Produce: schedule.release},
			{Delay: cleanupAt, Produce: schedule.cleanup},
		},
	)
	var cancel raknet.CancelSchedule
	if req.packet.ScheduleGroupResult != nil {
		cancel, err = req.packet.ScheduleGroupResult(producers, schedule.fail)
	} else if req.packet.ScheduleGroup != nil {
		cancel, err = req.packet.ScheduleGroup(producers)
	} else {
		err = errors.New("schedule unavailable")
	}
	if err == nil && cancel == nil {
		err = errors.New("channel cancellation unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		schedule.fail(err)
		return nil, fmt.Errorf("channelAreaSchedule: %w", err)
	}
	run.setCancel(cancel)
	r.logger.Printf(
		"RakNet hero channel area accepted ability=%s source=%d center=(%g,%g,%g)",
		definition.Name, req.command.Common.ObjectID, center.X, center.Y, center.Z,
	)
	packets := append([][]byte{start.Acknowledge, manaPacket}, start.Presentation...)
	admission := channelAreaAdmission{run: run, packets: [][]byte{createPacket, positionPacket, effectPacket}}
	err = req.packet.AfterResponseCommit(admission.commit)
	if err != nil {
		schedule.fail(err)
		return nil, fmt.Errorf("channelAreaPublication: %w", err)
	}
	return packets, nil
}
