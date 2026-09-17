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
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
)

type heroTimedAreaRun struct {
	objectID      uint32
	effectPool    *attachedEffectPool
	effectSlot    uint8
	isEffectBound bool
	removalPacket []byte
	cancel        raknet.CancelSchedule
}

func (e *heroTimedAreaRun) Stop() {
	if e == nil || e.cancel == nil {
		return
	}
	e.cancel()
	e.cancel = nil
}

func (e *heroTimedAreaRun) ReleaseEffect() bool {
	if e == nil || !e.isEffectBound {
		return false
	}
	isReleased := e.effectPool.Release(e.objectID, e.effectSlot)
	e.isEffectBound = false
	return isReleased
}

type heroTimedAreaSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	sourceObjectID      uint32
	creatureIndex       uint32
	previousManaPoint   float32
	creature            game.GameplayCreature
	definition          sim.AbilityDefinition
	binding             game.GameplayBinding
	run                 *heroTimedAreaRun
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releasePacket       []byte
}

type heroTimedAreaTickStep struct {
	schedule heroTimedAreaSchedule
	deadline time.Duration
	isFirst  bool
}

func (e heroTimedAreaTickStep) produce() ([][]byte, error) {
	return e.schedule.tick(e.deadline, e.isFirst)
}

func (e heroTimedAreaSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.heroTimedArea == e.run &&
		peerSession.deployedObjectID == e.sourceObjectID
}

func (e heroTimedAreaSchedule) tick(
	deadline time.Duration, isFirst bool,
) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	plan, err := zoneability.PlanArea(
		peerSession.zone.NPCs(), e.sourceObjectID,
		game.Vec3(peerSession.playerPosition), e.creature, e.definition,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("timedAreaPlan: %w", err)
	}
	results, err := zoneability.CommitArea(
		peerSession.zone.Population().Random(), peerSession.zone.NPCs(),
		plan, e.creature, peerSession.binding.Difficulty,
		e.runtime.program.Critical,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("timedAreaDamage: %w", err)
	}
	transitions := make([]campaignDamageTransition, 0, len(results))
	for index, result := range results {
		transition, transitionErr :=
			peerSession.applyCampaignDamageTransition(result.Damage)
		if transitionErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("timedAreaTransition[%d]: %w", index, transitionErr)
		}
		transitions = append(transitions, transition)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	packets, err := e.runtime.damage.publishAreaResults(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID,
		e.packet.SourceTime+uint64(deadline/time.Millisecond), e.binding,
		results, transitions, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("timedAreaPublish: %w", err)
	}
	if !isFirst {
		return packets, nil
	}
	effectPacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: e.run.effectSlot + 1, IsForceAttached: true,
		Asset:    util.HashID(e.definition.ActivationEffectName),
		ObjectID: e.sourceObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("timedAreaEffect: %w", err)
	}
	return append([][]byte{effectPacket, e.releasePacket}, packets...), nil
}

func (e heroTimedAreaSchedule) finish() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	peerSession.heroTimedArea = nil
	e.run.cancel = nil
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	e.run.ReleaseEffect()
	return [][]byte{e.run.removalPacket}, nil
}

func (e heroTimedAreaSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		peerSession.heroTimedArea = nil
		peerSession.queuePackets([][]byte{e.run.removalPacket})
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
	e.run.ReleaseEffect()
	e.runtime.logger.Printf(
		"RakNet hero timed area stopped after schedule failure for %s: %v",
		e.sessionKey, scheduleErr,
	)
}

func (r campaignAbilityCommandRuntime) handleHeroTimedArea(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	activeAbilityID uint32, sessionKey string, abilityStartTime time.Time,
) ([][]byte, error) {
	if activeAbilityID == 0 || definition.Kind != sim.AbilityKindTimedArea ||
		definition.Radius <= 0 || definition.HitDelay < 0 ||
		definition.Duration <= definition.HitDelay || definition.TickDuration <= 0 ||
		definition.NumberOfTicks == 0 || definition.MinimumDamage <= 0 ||
		definition.MaximumDamage < definition.MinimumDamage ||
		definition.AnimationName == "" || definition.ActivationEffectName == "" {
		r.registry.mutex.Unlock()
		return req.reject("timed area definition unavailable")
	}
	if peerSession.heroTimedArea != nil {
		r.registry.mutex.Unlock()
		return req.reject("timed area already active")
	}
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("timedAreaTiming: %w", err)
	}
	if projected.IsAreaDurationScaled {
		projected.Duration, err = game.ResolveAreaDuration(
			projected.Duration, creature.AreaDurationIncrease,
		)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("timedAreaDuration: %w", err)
		}
		projected.NumberOfTicks, err = game.ResolveAreaDurationCount(
			projected.NumberOfTicks, creature.AreaDurationIncrease,
		)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("timedAreaTickCount: %w", err)
		}
	}
	manaCost, err := game.ResolveAbilityManaCost(
		projected.ManaCost, creature.DamageProfile.PrimaryAttribute,
		projected.ManaCoefficient, peerSession.isOverdriveActiveAt(abilityStartTime),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("timedAreaMana: %w", err)
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}
	effectSlot, isEffectAllocated := r.effectPool.Allocate(req.command.Common.ObjectID)
	if !isEffectAllocated {
		r.registry.mutex.Unlock()
		return req.reject("timed area effect unavailable")
	}
	removalPacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: effectSlot + 1, IsRemovalRequested: true,
		IsHardStop: true, ObjectID: req.command.Common.ObjectID,
	})
	if err != nil {
		r.effectPool.Release(req.command.Common.ObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("timedAreaRemoval: %w", err)
	}
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	previousManaPoint := peerSession.deployedManaPoint()
	start, err := abilityraknet.StartAreaBasic(abilityraknet.AreaBasicStartRequest{
		SyncStamp: req.command.Common.Unknown[0], SourceID: req.command.Common.ObjectID,
		AbilityID: activeAbilityID, AbilityIndex: req.command.Ability.Index,
		SourceTime: req.packet.SourceTime, AnimationName: projected.AnimationName,
		HitDelay: projected.HitDelay, ReleaseDelay: projected.ReleaseDelay,
		Cooldown: projected.Cooldown,
	})
	if err != nil {
		r.effectPool.Release(req.command.Common.ObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("timedAreaStart: %w", err)
	}
	manaPacket, err := abilityraknet.Mana(
		req.command.Common.ObjectID, remainingManaPoint,
	)
	if err != nil {
		r.effectPool.Release(req.command.Common.ObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("timedAreaManaPacket: %w", err)
	}
	cooldownReservation, isCooldownReserved :=
		peerSession.abilityCooldownSession().Reserve(
			zoneability.HeroAbilityCooldown(activeAbilityID),
			abilityStartTime, projected.Cooldown,
		)
	if !isCooldownReserved {
		r.effectPool.Release(req.command.Common.ObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return req.reject("timed area cooldown unavailable")
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			abilityStartTime, projected.ReleaseDelay,
		)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		r.effectPool.Release(req.command.Common.ObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return req.reject("timed area release unavailable")
	}
	err = peerSession.setDeployedManaPoints(remainingManaPoint)
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.effectPool.Release(req.command.Common.ObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("timedAreaCommit: %w", err)
	}
	run := &heroTimedAreaRun{
		objectID: req.command.Common.ObjectID, effectPool: r.effectPool,
		effectSlot: effectSlot, isEffectBound: true,
		removalPacket: removalPacket,
	}
	peerSession.heroTimedArea = run
	generation := peerSession.generation
	binding := peerSession.binding
	creatureIndex := peerSession.deployedCreatureIndex
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	schedule := heroTimedAreaSchedule{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: req.command.Common.ObjectID,
		creatureIndex: creatureIndex, previousManaPoint: previousManaPoint,
		creature: creature, definition: projected, binding: binding, run: run,
		cooldownReservation: cooldownReservation,
		releaseReservation:  releaseReservation, releasePacket: start.Release,
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, projected.NumberOfTicks+1)
	for tick := uint32(0); tick < projected.NumberOfTicks; tick++ {
		deadline := projected.HitDelay + time.Duration(tick)*projected.TickDuration
		step := heroTimedAreaTickStep{
			schedule: schedule, deadline: deadline, isFirst: tick == 0,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: deadline, Produce: step.produce,
		})
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: projected.Duration, Produce: schedule.finish,
	})
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
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
		return nil, fmt.Errorf("timedAreaSchedule: %w", err)
	}
	run.cancel = cancel
	r.logger.Printf(
		"RakNet hero timed area accepted ability=%s source=%d ticks=%d",
		projected.Name, req.command.Common.ObjectID, projected.NumberOfTicks,
	)
	packets := append([][]byte{start.Acknowledge, manaPacket}, start.Presentation...)
	return packets, nil
}
