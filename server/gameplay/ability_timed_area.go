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
	isCommitted   bool
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
	sourceCreatureIndex uint32
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
		peerSession.deployedObjectID == e.sourceObjectID &&
		peerSession.deployedCreatureIndex == e.creatureIndex &&
		e.creatureIndex < uint32(len(peerSession.binding.Creatures)) &&
		e.sourceCreatureIndex < uint32(len(peerSession.binding.Creatures)) &&
		peerSession.binding.Creatures[e.creatureIndex].Noun == e.creature.Noun
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
	// Damage production can precede the initial transport write. Once a tick
	// starts, failure must not restore the cast's captured pre-payment state.
	e.run.isCommitted = true
	creature := e.creature
	if e.definition.Name == "PlasmaSentinelSupport" {
		// Cone template170 takes a new source snapshot inside every DoDamage.
		creature = e.runtime.registry.projectPassiveCreature(
			peerSession, e.sourceCreatureIndex, e.runtime.now(),
		)
	}
	if peerSession.binding.Mode == game.ModeArena {
		return e.tickArenaLocked(peerSession, creature, deadline, isFirst)
	}
	plan, err := zoneability.PlanArea(
		peerSession.zone.NPCs(), e.sourceObjectID,
		game.Vec3(peerSession.playerPosition), creature, e.definition,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("timedAreaPlan: %w", err)
	}
	results, err := zoneability.CommitArea(
		peerSession.zone.Population().Random(), peerSession.zone.NPCs(),
		plan, creature, peerSession.binding.Difficulty,
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

func (e heroTimedAreaSchedule) tickArenaLocked(
	peerSession gameplayPeerSession, creature game.GameplayCreature,
	deadline time.Duration, isFirst bool,
) ([][]byte, error) {
	timestamp := e.packet.SourceTime + uint64(deadline/time.Millisecond)
	targets := e.runtime.registry.abilityTargetsLocked(
		e.runtime, peerSession, e.definition, timestamp,
	).(*arenaAbilityTargets)
	plan, err := zoneability.PlanArea(
		targets, e.sourceObjectID, game.Vec3(peerSession.playerPosition),
		creature, e.definition,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaTimedAreaPlan: %w", err)
	}
	random, err := e.runtime.registry.abilityRandomLocked(peerSession)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaTimedAreaRandom: %w", err)
	}
	results, err := zoneability.CommitArea(
		random, targets, plan, creature, peerSession.binding.Difficulty,
		e.runtime.program.Critical,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("arenaTimedAreaDamage: %w", err)
	}
	packets, err := targets.packets(results)
	e.runtime.registry.mutex.Unlock()
	targets.finish()
	if err != nil {
		return nil, fmt.Errorf("arenaTimedAreaPackets: %w", err)
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
		return nil, fmt.Errorf("arenaTimedAreaEffect: %w", err)
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
		e.run.Stop()
		peerSession.queuePackets([][]byte{e.run.removalPacket})
		if !e.run.isCommitted {
			manaErr := peerSession.setCampaignCharacterManaPoints(
				e.creatureIndex, e.previousManaPoint,
			)
			if manaErr != nil {
				e.runtime.logger.Printf("RakNet timed area admission mana rollback failed for %s: %v", e.sessionKey, manaErr)
			}
			peerSession.abilityCooldownSession().Rollback(e.cooldownReservation)
			peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
		}
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return
	}
	e.runtime.registry.queuePeerPresentation(
		gameplayProducerIdentityFromSession(e.sessionKey, peerSession, true),
		[][]byte{e.run.removalPacket},
	)
	e.run.ReleaseEffect()
	e.runtime.logger.Printf(
		"RakNet hero timed area stopped after schedule failure for %s: %v",
		e.sessionKey, scheduleErr,
	)
}

func (e heroTimedAreaSchedule) commitAdmission() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if e.isCurrent(peerSession, isFound) {
		e.run.isCommitted = true
	}
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
	if projected.Name == "PlasmaSentinelSupport" {
		// Template170 uses two-argument native waits. Descriptor152 is not
		// Basic, and no third channel-scaling argument is supplied. Its custom
		// TimeToHitAction also releases the agent immediately at the raw hit.
		projected.HitDelay = definition.HitDelay
		projected.ReleaseDelay = definition.HitDelay
	}
	if projected.IsAreaDurationScaled {
		projected.Duration, err = game.ResolveAreaDuration(
			projected.Duration, creature.AreaDurationIncrease,
		)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("timedAreaDuration: %w", err)
		}
		if projected.Name == "PlasmaSentinelSupport" {
			projected.NumberOfTicks, err = plasmaSentinelTickCount(projected, definition.HitDelay)
		} else {
			projected.NumberOfTicks, err = game.ResolveAreaDurationCount(
				projected.NumberOfTicks, creature.AreaDurationIncrease,
			)
		}
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
	sourceCreatureIndex := creatureIndex
	if req.command.Ability.Index >= 6 && req.command.Ability.Index <= 8 {
		sourceCreatureIndex = req.command.Ability.Index - 6
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	schedule := heroTimedAreaSchedule{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: req.command.Common.ObjectID,
		creatureIndex: creatureIndex, sourceCreatureIndex: sourceCreatureIndex,
		previousManaPoint: previousManaPoint,
		creature:          creature, definition: projected, binding: binding, run: run,
		cooldownReservation: cooldownReservation,
		releaseReservation:  releaseReservation, releasePacket: start.Release,
	}
	err = req.packet.AfterResponseCommit(schedule.commitAdmission)
	if err != nil {
		schedule.fail(err)
		return nil, fmt.Errorf("timedAreaAcceptance: %w", err)
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
	if err == nil && cancel == nil {
		err = errors.New("nil timed area cancellation")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		schedule.fail(err)
		return nil, fmt.Errorf("timedAreaSchedule: %w", err)
	}
	r.registry.mutex.Lock()
	current, isFound := r.registry.sessions[sessionKey]
	isCurrent := schedule.isCurrent(current, isFound)
	if isCurrent {
		run.cancel = cancel
	}
	r.registry.mutex.Unlock()
	if !isCurrent {
		cancel()
		return nil, errors.New("timed area stopped during admission")
	}
	r.logger.Printf(
		"RakNet hero timed area accepted ability=%s source=%d ticks=%d",
		projected.Name, req.command.Common.ObjectID, projected.NumberOfTicks,
	)
	packets := append([][]byte{start.Acknowledge, manaPacket}, start.Presentation...)
	return packets, nil
}

func plasmaSentinelTickCount(definition sim.AbilityDefinition, authoredHitDelay time.Duration) (uint32, error) {
	remaining := definition.Duration - authoredHitDelay
	if remaining <= 0 || definition.TickDuration <= 0 {
		return 0, errors.New("invalid plasma sentinel duration")
	}
	// Template170 damages once, waits an interval, subtracts it, and repeats
	// only while the remaining duration is strictly greater than that interval.
	count := max(time.Duration(1), (remaining-1)/definition.TickDuration)
	if uint64(count) > uint64(^uint32(0)) {
		return 0, errors.New("plasma sentinel tick count overflow")
	}
	return uint32(count), nil
}
