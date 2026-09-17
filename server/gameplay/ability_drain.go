package gameplay

import (
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
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type heroDrainRun struct {
	mutex              sync.Mutex
	damageReduction    float32
	isProtectionActive bool
	effectPool         *attachedEffectPool
	sourceObjectID     uint32
	targetObjectID     uint32
	sourceEffectSlot   uint8
	targetEffectSlot   uint8
	areEffectsAttached bool
	isEffectsReleased  bool
	releasePacket      []byte
	cancel             raknet.CancelSchedule
}

func (e *heroDrainRun) Stop() {
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
	e.releaseEffects()
}

func (e *heroDrainRun) attachEffects(
	beamAsset string, impactAsset string,
) ([][]byte, error) {
	if e == nil {
		return nil, nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.areEffectsAttached || e.isEffectsReleased {
		return nil, nil
	}
	packets, err := abilityraknet.ChannelDrainAttachedEffects(
		beamAsset, impactAsset, e.sourceObjectID, e.sourceEffectSlot,
		e.targetObjectID, e.targetEffectSlot,
	)
	if err != nil {
		return nil, fmt.Errorf("heroDrainAttach: %w", err)
	}
	e.areEffectsAttached = true
	return packets, nil
}

func (e *heroDrainRun) setCancel(cancel raknet.CancelSchedule) {
	if e == nil {
		return
	}
	e.mutex.Lock()
	e.cancel = cancel
	e.mutex.Unlock()
}

func (e *heroDrainRun) clearCancel() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	e.cancel = nil
	e.mutex.Unlock()
}

func (e *heroDrainRun) releaseEffects() bool {
	if e == nil {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isEffectsReleased {
		return false
	}
	e.isEffectsReleased = true
	e.effectPool.Release(e.sourceObjectID, e.sourceEffectSlot)
	e.effectPool.Release(e.targetObjectID, e.targetEffectSlot)
	return e.areEffectsAttached
}

func (e *heroDrainRun) interruptionPackets() ([][]byte, error) {
	if e == nil {
		return nil, nil
	}
	isAttached := e.releaseEffects()
	packets := make([][]byte, 0, 3)
	if isAttached {
		removals, err := abilityraknet.ChannelDrainEffectRemovals(
			e.sourceObjectID, e.sourceEffectSlot,
			e.targetObjectID, e.targetEffectSlot,
		)
		if err != nil {
			return nil, fmt.Errorf("heroDrainInterruptEffects: %w", err)
		}
		packets = append(packets, removals...)
	}
	if len(e.releasePacket) > 0 {
		packets = append(packets, e.releasePacket)
	}
	return packets, nil
}

func (e *heroDrainRun) interruptionPacketsAt(timestamp uint64) ([][]byte, error) {
	packets, err := e.interruptionPackets()
	if err != nil {
		return nil, fmt.Errorf("heroDrainInterrupt: %w", err)
	}
	animationPacket, err := abilityraknet.AnimationReset(e.sourceObjectID, timestamp)
	if err != nil {
		return nil, fmt.Errorf("heroDrainInterruptAnimation: %w", err)
	}
	return append(packets, animationPacket), nil
}

type heroDrainSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	sourceObjectID      uint32
	targetObjectID      uint32
	creatureIndex       uint32
	previousManaPoint   float32
	creature            game.GameplayCreature
	definition          sim.AbilityDefinition
	binding             game.GameplayBinding
	run                 *heroDrainRun
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releasePacket       []byte
}

type heroDrainTickStep struct {
	schedule heroDrainSchedule
	deadline time.Duration
}

func (e heroDrainTickStep) produce() ([][]byte, error) {
	return e.schedule.tick(e.deadline)
}

func (e heroDrainSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.heroDrain == e.run &&
		peerSession.deployedObjectID == e.sourceObjectID
}

func (e heroDrainSchedule) tick(deadline time.Duration) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.targetObjectID)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 ||
		target.Faction != zonenpc.FactionNonPlayerAligned ||
		!isInsideZoneTrigger(
			peerSession.playerPosition,
			raknet.Vector3{
				X: target.Plan.Position.X,
				Y: target.Plan.Position.Y,
				Z: target.Plan.Position.Z,
			},
			e.definition.Range,
		) {
		peerSession.heroDrain = nil
		peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
		e.run.cancel = nil
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		return e.endPackets(deadline)
	}
	e.run.isProtectionActive = true
	tickDefinition := e.definition
	tickDefinition.MinimumDamage = e.definition.MinimumDamagePerTick
	tickDefinition.MaximumDamage = e.definition.MaximumDamagePerTick
	damage, err := zoneability.ProjectDamage(
		e.creature, tickDefinition,
		tickDefinition.MinimumDamage, tickDefinition.MaximumDamage,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroDrainDamageProjection: %w", err)
	}
	results, err := zoneability.CommitArea(
		peerSession.zone.Population().Random(), peerSession.zone.NPCs(),
		zoneability.AreaPlan{
			SourceObjectID: e.sourceObjectID,
			AbilityID:      util.HashID(e.definition.Name),
			Definition:     e.definition,
			Damage:         damage,
			Target:         []zonenpc.Snapshot{target},
		},
		e.creature, peerSession.binding.Difficulty,
		e.runtime.program.Critical,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroDrainDamage: %w", err)
	}
	if len(results) == 0 || results[0].Damage.Damage <= 0 {
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	transition, err := peerSession.applyCampaignDamageTransition(results[0].Damage)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroDrainTransition: %w", err)
	}
	healing, err := zoneability.ProjectHealing(
		e.creature, e.definition, e.definition.MinimumHealingPerTick,
	)
	if err == nil {
		healing, err = game.ApplyTargetHealingReduction(
			healing, e.creature.HealingTargetProfile,
		)
	}
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroDrainHealingProjection: %w", err)
	}
	maximumHitPoint, _, err := peerSession.deployedResourceMaximum()
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroDrainMaximum: %w", err)
	}
	previousHitPoint := peerSession.deployedHitPoint()
	hitPoint := min(maximumHitPoint, previousHitPoint+healing)
	healedAmount := hitPoint - previousHitPoint
	if healedAmount > 0 {
		_, err = peerSession.setDeployedHitPoints(hitPoint)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroDrainHealing: %w", err)
		}
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	effectPackets, err := e.run.attachEffects(
		e.definition.MuzzleEffectName, e.definition.HitEffectName,
	)
	if err != nil {
		return nil, fmt.Errorf("heroDrainEffects: %w", err)
	}
	damagePackets, err := e.runtime.damage.publishAreaResults(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID,
		e.packet.SourceTime+uint64(deadline/time.Millisecond), e.binding,
		results, []campaignDamageTransition{transition}, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("heroDrainPublish: %w", err)
	}
	packets := append(effectPackets, damagePackets...)
	if healedAmount > 0 {
		healingPackets, healingErr := abilityraknet.ChannelDrainHealing(
			e.sourceObjectID, hitPoint, healedAmount,
		)
		if healingErr != nil {
			return nil, fmt.Errorf("heroDrainHealingMarshal: %w", healingErr)
		}
		packets = append(packets, healingPackets...)
	}
	return packets, nil
}

func (e heroDrainSchedule) endPackets(deadline time.Duration) ([][]byte, error) {
	cleanupPackets, err := e.run.interruptionPackets()
	if err != nil {
		return nil, fmt.Errorf("heroDrainEndEffects: %w", err)
	}
	animationPacket, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: e.sourceObjectID, State: util.HashID(e.definition.OutAnimationName),
		Timestamp: e.packet.SourceTime + uint64(deadline/time.Millisecond), Scale: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("heroDrainEndAnimation: %w", err)
	}
	return append(cleanupPackets, animationPacket), nil
}

func (e heroDrainSchedule) finish() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	peerSession.heroDrain = nil
	e.run.clearCancel()
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	return e.endPackets(e.definition.ReleaseDelay)
}

func (e heroDrainSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		peerSession.heroDrain = nil
		_ = peerSession.setCampaignCharacterManaPoints(
			e.creatureIndex, e.previousManaPoint,
		)
		peerSession.abilityCooldownSession().Rollback(e.cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	e.run.releaseEffects()
	if isCurrent {
		e.runtime.logger.Printf(
			"RakNet hero channel drain stopped after schedule failure for %s: %v",
			e.sessionKey, scheduleErr,
		)
	}
}

func (r campaignAbilityCommandRuntime) handleHeroChannelDrain(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	activeAbilityID uint32, targetObjectID uint32, sessionKey string,
	abilityStartTime time.Time,
) ([][]byte, error) {
	if activeAbilityID == 0 || definition.Kind != sim.AbilityKindChannelDrain ||
		definition.Range <= 0 || definition.NumberOfTicks == 0 ||
		definition.TickDuration <= 0 || definition.MinimumDamagePerTick <= 0 ||
		definition.MaximumDamagePerTick < definition.MinimumDamagePerTick ||
		definition.MinimumHealingPerTick <= 0 ||
		definition.MaximumHealingPerTick < definition.MinimumHealingPerTick ||
		definition.AnimationName == "" || definition.OutAnimationName == "" ||
		definition.MuzzleEffectName == "" || definition.HitEffectName == "" {
		r.registry.mutex.Unlock()
		return req.reject("channel drain definition unavailable")
	}
	if peerSession.heroDrain != nil {
		r.registry.mutex.Unlock()
		return req.reject("channel drain already active")
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(targetObjectID)
	admissionRange := heroAbilityAdmissionRange(creature, definition)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 ||
		target.Faction != zonenpc.FactionNonPlayerAligned ||
		!isInsideZoneTrigger(
			peerSession.playerPosition,
			raknet.Vector3{
				X: target.Plan.Position.X,
				Y: target.Plan.Position.Y,
				Z: target.Plan.Position.Z,
			},
			admissionRange,
		) {
		r.registry.mutex.Unlock()
		return req.reject("channel drain target unavailable")
	}
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroDrainTiming: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(
		projected.ManaCost, creature.DamageProfile.PrimaryAttribute,
		projected.ManaCoefficient, peerSession.isOverdriveActiveAt(abilityStartTime),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroDrainMana: %w", err)
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	previousManaPoint := peerSession.deployedManaPoint()
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: req.command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: activeAbilityID, AbilityIndex: req.command.Ability.Index,
		SourceStartMilliseconds: req.packet.SourceTime,
		SourceCommitMilliseconds: req.packet.SourceTime +
			uint64(projected.HitDelay/time.Millisecond),
		SourceEndMilliseconds: req.packet.SourceTime +
			uint64(projected.ReleaseDelay/time.Millisecond),
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroDrainAck: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(
		req.command.Common.Unknown[0], activeAbilityID,
		req.command.Ability.Index, req.packet.SourceTime,
		projected.HitDelay, projected.ReleaseDelay,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroDrainRelease: %w", err)
	}
	messages := []raknet.ApplicationMessage{
		raknet.SetAnimationStateMessage{
			ObjectID:  req.command.Common.ObjectID,
			State:     util.HashID(projected.AnimationName),
			Timestamp: req.packet.SourceTime, Scale: 1,
		},
		raknet.CooldownUpdateMessage{
			ObjectID: req.command.Common.ObjectID, AbilityKey: uint64(activeAbilityID),
			DurationMilliseconds: projected.Cooldown.Milliseconds(),
			SourceStartMilliseconds: int64(req.packet.SourceTime) +
				projected.HitDelay.Milliseconds(),
		},
		raknet.CombatantDataDeltaMessage{
			ObjectID: req.command.Common.ObjectID, ManaPoints: remainingManaPoint,
			IsManaPointChanged: true,
		},
	}
	startPackets := make([][]byte, 0, len(messages)+1)
	for index, message := range messages {
		packet, marshalErr := raknet.MarshalApplication(message)
		if marshalErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroDrainStart[%d]: %w", index, marshalErr)
		}
		startPackets = append(startPackets, packet)
	}
	cooldownReservation, isCooldownReserved :=
		peerSession.abilityCooldownSession().Reserve(
			zoneability.HeroAbilityCooldown(activeAbilityID),
			abilityStartTime, projected.Cooldown,
		)
	if !isCooldownReserved {
		r.registry.mutex.Unlock()
		return req.reject("channel drain cooldown unavailable")
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			abilityStartTime, projected.ReleaseDelay,
		)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		r.registry.mutex.Unlock()
		return req.reject("channel drain release unavailable")
	}
	sourceEffectSlot, isSourceEffectAllocated := r.effectPool.Allocate(
		req.command.Common.ObjectID,
	)
	if !isSourceEffectAllocated {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return req.reject("channel drain effect unavailable")
	}
	targetEffectSlot, isTargetEffectAllocated := r.effectPool.Allocate(targetObjectID)
	if !isTargetEffectAllocated {
		r.effectPool.Release(req.command.Common.ObjectID, sourceEffectSlot)
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return req.reject("channel drain target effect unavailable")
	}
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		r.effectPool.Release(req.command.Common.ObjectID, sourceEffectSlot)
		r.effectPool.Release(targetObjectID, targetEffectSlot)
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroDrainCommit: %w", err)
	}
	run := &heroDrainRun{
		effectPool: r.effectPool, sourceObjectID: req.command.Common.ObjectID,
		targetObjectID: targetObjectID, sourceEffectSlot: sourceEffectSlot,
		targetEffectSlot: targetEffectSlot, releasePacket: releasePacket,
	}
	if projected.Name == "NecroRandom" {
		run.damageReduction = projected.StatusDamageReduction
	}
	peerSession.heroDrain = run
	generation := peerSession.generation
	binding := peerSession.binding
	creatureIndex := peerSession.deployedCreatureIndex
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	schedule := heroDrainSchedule{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: req.command.Common.ObjectID,
		targetObjectID: targetObjectID, creatureIndex: creatureIndex,
		previousManaPoint: previousManaPoint, creature: creature,
		definition: projected, binding: binding, run: run,
		cooldownReservation: cooldownReservation,
		releaseReservation:  releaseReservation, releasePacket: releasePacket,
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, projected.NumberOfTicks+1)
	for tick := uint32(0); tick < projected.NumberOfTicks; tick++ {
		deadline := projected.HitDelay + time.Duration(tick)*projected.TickDuration
		step := heroDrainTickStep{schedule: schedule, deadline: deadline}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: deadline, Produce: step.produce,
		})
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: projected.ReleaseDelay, Produce: schedule.finish,
	})
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if req.packet.ScheduleGroupResult != nil {
		cancel, err = req.packet.ScheduleGroupResult(producers, schedule.fail)
	} else {
		cancel, err = req.packet.ScheduleGroup(producers)
	}
	if err != nil {
		schedule.fail(err)
		return nil, fmt.Errorf("heroDrainSchedule: %w", err)
	}
	run.setCancel(cancel)
	r.logger.Printf(
		"RakNet hero channel drain accepted ability=%s source=%d target=%d ticks=%d",
		projected.Name, req.command.Common.ObjectID, targetObjectID,
		projected.NumberOfTicks,
	)
	return append([][]byte{ackPacket}, startPackets...), nil
}
