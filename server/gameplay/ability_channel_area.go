package gameplay

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const gravityStormSlamDuration = 1200 * time.Millisecond
const gravityStormEffectShutdownDelay = 75 * time.Millisecond
const gravityStormEffectDeleteDelay = 2 * time.Second

type heroChannelAreaTarget struct {
	snapshot   zonenpc.Snapshot
	instanceID uint32
	expiresAt  time.Time
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
		_ = e.modifierPool.Release(target.instanceID)
	}
}

func (e *heroChannelAreaRun) Cleanup() []heroChannelAreaTarget {
	if e == nil {
		return nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isCleaned {
		return nil
	}
	e.isCleaned = true
	targets := e.targets
	e.targets = nil
	e.ReleaseTargets(targets)
	return targets
}

func (e *heroChannelAreaRun) Stop() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	cancel := e.cancel
	e.cancel = nil
	e.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
	e.Cleanup()
	e.ReleaseEffect()
	e.DeleteEffectObject()
}

func (e *heroChannelAreaRun) ReleaseEffect() bool {
	if e == nil {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if !e.isEffectBound {
		return false
	}
	isReleased := e.effectPool.Release(e.effectObjectID, e.effectSlot)
	e.isEffectBound = false
	return isReleased
}

func (e *heroChannelAreaRun) DeleteEffectObject() bool {
	if e == nil {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isEffectObjectDeleted || e.effectObjectID == 0 {
		return false
	}
	e.isEffectObjectDeleted = true
	return true
}

type heroChannelAreaSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	sourceObjectID      uint32
	creatureIndex       uint32
	previousManaPoint   float32
	center              raknet.Vector3
	creature            game.GameplayCreature
	definition          sim.AbilityDefinition
	binding             game.GameplayBinding
	run                 *heroChannelAreaRun
	slamStart           time.Duration
	slamHit             time.Duration
	cleanupAt           time.Duration
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releasePacket       []byte
}

func (e heroChannelAreaSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
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
	expiresAt := e.runtime.now().Add(e.cleanupAt - e.definition.HitDelay)
	targets := make([]heroChannelAreaTarget, 0)
	messages := make([]raknet.ApplicationMessage, 0)
	for index, target := range peerSession.zone.NPCs().LiveSnapshots() {
		if target.Faction != zonenpc.FactionNonPlayerAligned ||
			target.Plan.IsFixture ||
			zonegeometry.Distance(game.Vec3(e.center), target.Plan.Position) >
				e.definition.Radius {
			continue
		}
		instance, err := e.runtime.modifierPool.Allocate()
		if err != nil {
			e.run.ReleaseTargets(targets)
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("channelAreaModifier[%d]: %w", index, err)
		}
		err = peerSession.zone.NPCs().ApplyStun(target.Plan.ObjectID, expiresAt)
		if err != nil {
			_ = e.runtime.modifierPool.Release(instance)
			e.run.ReleaseTargets(targets)
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("channelAreaStun[%d]: %w", index, err)
		}
		targets = append(targets, heroChannelAreaTarget{
			snapshot: target, instanceID: instance, expiresAt: expiresAt,
		})
		messages = append(messages, raknet.ModifierCreatedMessage{
			TargetID: target.Plan.ObjectID, ModifierGUID: e.definition.RootModifierID,
			InstanceID: instance, StackCount: 1,
			DurationMilliseconds: uint32((e.cleanupAt - e.definition.HitDelay).Milliseconds()),
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
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("channelAreaLiftMarshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e heroChannelAreaSchedule) startSlam() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.RUnlock()
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
	return [][]byte{packet}, nil
}

func (e heroChannelAreaSchedule) shutdownEffect() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent || !e.run.ReleaseEffect() {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: e.run.effectSlot + 1, IsRemovalRequested: true,
		ObjectID: e.run.effectObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("channelAreaEffectShutdown: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e heroChannelAreaSchedule) deleteEffectObject() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent || !e.run.DeleteEffectObject() {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
		ObjectID: []uint32{e.run.effectObjectID},
	})
	if err != nil {
		return nil, fmt.Errorf("channelAreaEffectDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e heroChannelAreaSchedule) slam() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	targets := e.run.Targets()
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
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.releasePacket}, nil
}

func (e heroChannelAreaSchedule) cleanup() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if e.isCurrent(peerSession, isFound) {
		peerSession.heroChannelArea = nil
		e.run.mutex.Lock()
		e.run.cancel = nil
		e.run.mutex.Unlock()
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	targets := e.run.Cleanup()
	packets := make([][]byte, 0, len(targets))
	for index, target := range targets {
		packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{
			TargetID: target.snapshot.Plan.ObjectID, InstanceID: target.instanceID,
		})
		if err != nil {
			return nil, fmt.Errorf("channelAreaCleanup[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	if e.run.ReleaseEffect() {
		packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: e.run.effectSlot + 1, IsRemovalRequested: true,
			IsHardStop: true, ObjectID: e.run.effectObjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("channelAreaCleanupEffect: %w", err)
		}
		packets = append(packets, packet)
	}
	if e.run.DeleteEffectObject() {
		packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
			ObjectID: []uint32{e.run.effectObjectID},
		})
		if err != nil {
			return nil, fmt.Errorf("channelAreaCleanupObject: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e heroChannelAreaSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		peerSession.heroChannelArea = nil
		_ = peerSession.setCampaignCharacterManaPoints(
			e.creatureIndex, e.previousManaPoint,
		)
		peerSession.abilityCooldownSession().Rollback(e.cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return
	}
	e.run.Stop()
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
	previousManaPoint := peerSession.deployedManaPoint()
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		r.effectPool.Release(effectObjectID, effectSlot)
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("channelAreaCommit: %w", err)
	}
	run := &heroChannelAreaRun{
		npc: peerSession.zone.NPCs(), modifierPool: r.modifierPool,
		effectPool: r.effectPool, effectObjectID: effectObjectID,
		effectSlot: effectSlot, isEffectBound: true,
	}
	peerSession.heroChannelArea = run
	generation := peerSession.generation
	creatureIndex := peerSession.deployedCreatureIndex
	binding := peerSession.binding
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	schedule := heroChannelAreaSchedule{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: req.command.Common.ObjectID,
		creatureIndex: creatureIndex, previousManaPoint: previousManaPoint,
		center: center, creature: creature, definition: definition, binding: binding,
		run: run, slamStart: slamStart, slamHit: slamHit, cleanupAt: cleanupAt,
		cooldownReservation: cooldownReservation,
		releaseReservation:  releaseReservation, releasePacket: start.Release,
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
	if err != nil {
		schedule.fail(err)
		return nil, fmt.Errorf("channelAreaSchedule: %w", err)
	}
	run.mutex.Lock()
	run.cancel = cancel
	run.mutex.Unlock()
	r.logger.Printf(
		"RakNet hero channel area accepted ability=%s source=%d center=(%g,%g,%g)",
		definition.Name, req.command.Common.ObjectID, center.X, center.Y, center.Z,
	)
	packets := append([][]byte{start.Acknowledge, manaPacket}, start.Presentation...)
	packets = append(packets, createPacket, effectPacket)
	return packets, nil
}
