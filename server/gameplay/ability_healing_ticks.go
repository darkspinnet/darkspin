package gameplay

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
)

type heroHealingTicksRun struct {
	mutex      sync.Mutex
	cancel     raknet.CancelSchedule
	effectPool *attachedEffectPool
	effects    []fieldMedicEffectLease
}

type fieldMedicEffectLease struct {
	objectID uint32
	slot     uint8
}

func (e *heroHealingTicksRun) Stop() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	cancel := e.cancel
	e.cancel = nil
	e.releaseEffectsLocked()
	e.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (e *heroHealingTicksRun) ReleaseEffects() []fieldMedicEffectLease {
	if e == nil {
		return nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.releaseEffectsLocked()
}

func (e *heroHealingTicksRun) releaseEffectsLocked() []fieldMedicEffectLease {
	if len(e.effects) == 0 {
		return nil
	}
	releasedEffects := make([]fieldMedicEffectLease, 0, len(e.effects))
	for _, effect := range e.effects {
		if e.effectPool.Release(effect.objectID, effect.slot) {
			releasedEffects = append(releasedEffects, effect)
		}
	}
	e.effects = nil
	return releasedEffects
}

type heroHealingTicksSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	sourceObjectID      uint32
	creatureIndex       uint32
	previousManaPoint   float32
	position            raknet.Vector3
	creature            game.GameplayCreature
	definition          sim.AbilityDefinition
	binding             game.GameplayBinding
	target              fieldMedicHealingTarget
	run                 *heroHealingTicksRun
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releasePacket       []byte
	presentationPackets [][]byte
}

type heroHealingTicksStep struct {
	schedule heroHealingTicksSchedule
	deadline time.Duration
	isFirst  bool
	isFinal  bool
}

func (e heroHealingTicksStep) produce() ([][]byte, error) {
	return e.schedule.tick(e.deadline, e.isFirst, e.isFinal)
}

func (e heroHealingTicksSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.heroHealingTicks == e.run &&
		peerSession.deployedObjectID == e.sourceObjectID
}

func (e heroHealingTicksSchedule) stopLocked(
	peerSession gameplayPeerSession,
) raknet.CancelSchedule {
	peerSession.heroHealingTicks = nil
	cancel := e.run.cancel
	e.run.cancel = nil
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	return cancel
}

func (e heroHealingTicksSchedule) stopPackets(
	cancel raknet.CancelSchedule, deadline time.Duration,
) [][]byte {
	if cancel != nil {
		cancel()
	}
	packets := make([][]byte, 0, 2)
	if len(e.releasePacket) != 0 {
		packets = append(packets, e.releasePacket)
	}
	packets = append(packets, e.fieldMedicCleanupPackets(deadline)...)
	return packets
}

func (e heroHealingTicksSchedule) fieldMedicCleanupPackets(
	deadline time.Duration,
) [][]byte {
	if e.definition.Name != "FieldMedicSupport" {
		return nil
	}
	packets := make([][]byte, 0, 2)
	resetPacket, err := abilityraknet.AnimationReset(
		e.sourceObjectID,
		e.packet.SourceTime+uint64(deadline/time.Millisecond),
	)
	if err != nil {
		e.runtime.logger.Printf("RakNet Field Medic animation reset failed: %v", err)
	} else {
		packets = append(packets, resetPacket)
	}
	packets = append(packets, e.removeEffectPackets()...)
	return packets
}

func (e heroHealingTicksSchedule) removeEffectPackets() [][]byte {
	effects := e.run.ReleaseEffects()
	packets := make([][]byte, 0, len(effects))
	for _, effect := range effects {
		packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: effect.slot + 1, IsRemovalRequested: true,
			IsHardStop: true, ObjectID: effect.objectID,
		})
		if err != nil {
			e.runtime.logger.Printf(
				"RakNet healing ticks effect removal failed object=%d slot=%d: %v",
				effect.objectID, effect.slot, err,
			)
			continue
		}
		packets = append(packets, packet)
	}
	return packets
}

func (e heroHealingTicksSchedule) tick(
	deadline time.Duration, isFirst bool, isFinal bool,
) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	targetSession := peerSession
	if e.target.sessionKey != "" && e.target.sessionKey != e.sessionKey {
		var isTargetFound bool
		targetSession, isTargetFound =
			e.runtime.registry.sessions[e.target.sessionKey]
		if !isTargetFound || targetSession.generation != e.target.generation ||
			(!e.target.isCompanion &&
				targetSession.deployedObjectID != e.target.objectID) {
			cancel := e.stopLocked(peerSession)
			e.runtime.registry.mutex.Unlock()
			return e.stopPackets(cancel, deadline), nil
		}
	}
	hitPointBefore := float32(0)
	maximumHitPoint := float32(0)
	healingTargetProfile := game.HealingTargetProfile{}
	if e.target.isCompanion {
		companion, isCompanionFound :=
			targetSession.zone.Companion().Snapshot(e.target.objectID)
		if !isCompanionFound || !companion.IsTargetable || companion.HitPoint <= 0 {
			cancel := e.stopLocked(peerSession)
			e.runtime.registry.mutex.Unlock()
			return e.stopPackets(cancel, deadline), nil
		}
		hitPointBefore = companion.HitPoint
		maximumHitPoint = companion.MaximumHitPoint
	} else {
		if targetSession.squad == nil {
			cancel := e.stopLocked(peerSession)
			e.runtime.registry.mutex.Unlock()
			return e.stopPackets(cancel, deadline), nil
		}
		character, isCharacterFound :=
			targetSession.squad.Character(e.target.creatureIndex)
		if !isCharacterFound || !character.IsAvailable || character.HitPoints <= 0 {
			cancel := e.stopLocked(peerSession)
			e.runtime.registry.mutex.Unlock()
			return e.stopPackets(cancel, deadline), nil
		}
		hitPointBefore = character.HitPoints
		maximumHitPoint, _ =
			targetSession.characterResourceMaximum(e.target.creatureIndex)
		healingTargetProfile =
			targetSession.binding.Creatures[e.target.creatureIndex].HealingTargetProfile
	}
	healing, err := zoneability.ProjectHealing(
		e.creature, e.definition, e.definition.MinimumHealingPerTick,
	)
	if err == nil {
		healing, err = game.ApplyTargetHealingReduction(
			healing, healingTargetProfile,
		)
	}
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("healingTicksProjection: %w", err)
	}
	hitPoint := min(maximumHitPoint, hitPointBefore+healing)
	healedAmount := hitPoint - hitPointBefore
	if healedAmount > 0 {
		if e.target.isCompanion {
			_, _, err = targetSession.zone.Companion().SetHitPoint(
				e.target.objectID, hitPoint,
			)
		} else {
			_, err = targetSession.setCampaignCharacterHitPoints(
				e.target.creatureIndex, hitPoint,
			)
		}
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("healingTicksCommit: %w", err)
		}
	}
	isRemoteTarget := e.target.sessionKey != e.sessionKey
	if e.target.isCompanion && isRemoteTarget {
		targetSession.zone.PublishCompanionResourceTo(
			e.target.userID, e.target.generation, e.target.objectID,
		)
	}
	if isRemoteTarget {
		if !e.target.isCompanion {
			targetSession.zone.PublishHeroResourceTo(
				e.target.userID, e.target.generation,
			)
		}
		e.runtime.registry.sessions[e.target.sessionKey] = targetSession
	} else {
		peerSession = targetSession
	}
	if isFinal {
		peerSession.heroHealingTicks = nil
		e.run.cancel = nil
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	packets := make([][]byte, 0, 5)
	if isFirst {
		packets = append(packets, e.presentationPackets...)
	}
	if isFirst && e.definition.Name != "FieldMedicSupport" &&
		e.definition.HitEffectName != "" {
		effectPacket, marshalErr := raknet.MarshalApplication(
			raknet.PositionedEffectMessage{
				Asset: util.HashID(e.definition.HitEffectName), Position: e.position,
			},
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("healingTicksEffect: %w", marshalErr)
		}
		packets = append(packets, effectPacket)
	}
	if isFirst && e.definition.Name == "Sporogenesis" {
		purgePackets, err := e.runtime.purgeHeroDebuffs(
			e.sessionKey, e.generation, e.sourceObjectID,
		)
		if err != nil {
			return nil, fmt.Errorf("healingTicksPurge: %w", err)
		}
		packets = append(packets, purgePackets...)
	}
	if healedAmount <= 0 {
		return packets, nil
	}
	healingPackets, err := abilityraknet.MarshalTreeOfLifeHealing(
		e.definition, e.position,
		[]abilityraknet.Healing{{
			SourceObjectID: e.sourceObjectID, ObjectID: e.target.objectID,
			HitPoint: hitPoint, Amount: healedAmount,
		}}, isFinal,
	)
	if err != nil {
		return nil, fmt.Errorf("healingTicksMarshal: %w", err)
	}
	resourcePacket, err := e.resourcePacket()
	if err != nil {
		return nil, fmt.Errorf("healingTicksResource: %w", err)
	}
	packets = append(packets, healingPackets...)
	packets = append(packets, resourcePacket)
	err = e.runtime.stats.Record(
		context.Background(), e.binding,
		sporenet.PlayerStatDelta{
			PVEHealing: float64(healedAmount),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("healingTicksStats: %w", err)
	}
	if e.target.userID == e.binding.UserID {
		err = e.runtime.stats.Record(
			context.Background(), e.binding,
			sporenet.PlayerStatDelta{PVEHealingReceived: float64(healedAmount)},
		)
	} else {
		err = e.runtime.stats.Record(
			context.Background(), e.target.binding,
			sporenet.PlayerStatDelta{PVEHealingReceived: float64(healedAmount)},
		)
	}
	if err != nil {
		return nil, fmt.Errorf("healingTicksTargetStats: %w", err)
	}
	return packets, nil
}

func (e heroHealingTicksSchedule) resourcePacket() ([]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.target.sessionKey]
	if !isFound || peerSession.generation != e.target.generation {
		e.runtime.registry.mutex.RUnlock()
		return nil, errors.New("healing ticks session missing")
	}
	var packet []byte
	var err error
	if e.target.isCompanion {
		companion, isCompanionFound :=
			peerSession.zone.Companion().Snapshot(e.target.objectID)
		if !isCompanionFound {
			e.runtime.registry.mutex.RUnlock()
			return nil, errors.New("healing ticks companion missing")
		}
		packet, err = raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
			ObjectID: companion.ObjectID, HitPoints: companion.HitPoint,
			IsHitPointChanged: true,
		})
	} else {
		packet, err = peerSession.marshalCampaignCharacterResource(
			e.target.creatureIndex,
		)
	}
	e.runtime.registry.mutex.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("resourceMarshal: %w", err)
	}
	return packet, nil
}

func (e heroHealingTicksSchedule) release() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets := make([][]byte, 0, 3)
	packets = append(packets, e.releasePacket)
	packets = append(packets, e.fieldMedicCleanupPackets(e.definition.ReleaseDelay)...)
	return packets, nil
}

func (e heroHealingTicksSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		peerSession.heroHealingTicks = nil
		_ = peerSession.setCampaignCharacterManaPoints(
			e.creatureIndex, e.previousManaPoint,
		)
		peerSession.abilityCooldownSession().Rollback(e.cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	e.run.ReleaseEffects()
	if !isCurrent {
		return
	}
	e.runtime.logger.Printf(
		"RakNet hero healing ticks stopped after schedule failure for %s: %v",
		e.sessionKey, scheduleErr,
	)
}

func (r campaignAbilityCommandRuntime) handleHeroHealingTicks(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	activeAbilityID uint32, targetObjectID uint32, sessionKey string,
	abilityStartTime time.Time,
) ([][]byte, error) {
	if activeAbilityID == 0 || definition.Kind != sim.AbilityKindHealingTicks ||
		peerSession.squad == nil ||
		definition.NumberOfTicks == 0 || definition.TickDuration <= 0 ||
		definition.MinimumHealingPerTick <= 0 ||
		definition.MaximumHealingPerTick != definition.MinimumHealingPerTick ||
		definition.AnimationName == "" || definition.HealEffectName == "" {
		r.registry.mutex.Unlock()
		return req.reject("healing ticks definition unavailable")
	}
	if peerSession.heroHealingTicks != nil {
		r.registry.mutex.Unlock()
		return req.reject("healing ticks already active")
	}
	target := fieldMedicHealingTarget{
		sessionKey: sessionKey, generation: peerSession.generation,
		userID: peerSession.binding.UserID, objectID: peerSession.deployedObjectID,
		creatureIndex: peerSession.deployedCreatureIndex, binding: peerSession.binding,
		position: game.Vec3(peerSession.playerPosition),
	}
	if definition.Name == "FieldMedicSupport" {
		admissionRange := heroAbilityAdmissionRange(creature, definition)
		target = fieldMedicSupportTargetLocked(
			r.registry, sessionKey, peerSession, targetObjectID,
			game.Vec3(req.command.Ability.CursorPosition), admissionRange,
		)
		targetObjectID = target.objectID
	}
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("healingTicksTiming: %w", err)
	}
	isFieldMedicSupport := projected.Name == "FieldMedicSupport"
	isFieldMedicAlly := isFieldMedicSupport && target.objectID != req.command.Common.ObjectID
	if isFieldMedicSupport {
		projected.MuzzleEffectName = ""
		if isFieldMedicAlly {
			projected.AnimationName = "cast_fieldmedicsupport"
			projected.HitEffectName = "effect_fieldmedicsupport_target.ServerEventDef"
		} else {
			projected.AnimationName = "cast_fieldmedicsupport_self"
			projected.HitEffectName = "effect_fieldmedicsupport_targetself.ServerEventDef"
		}
	}
	manaCost, err := game.ResolveAbilityManaCost(
		projected.ManaCost, creature.DamageProfile.PrimaryAttribute,
		projected.ManaCoefficient, peerSession.isOverdriveActiveAt(abilityStartTime),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("healingTicksMana: %w", err)
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	previousManaPoint := peerSession.deployedManaPoint()
	start, err := abilityraknet.StartAreaBasic(abilityraknet.AreaBasicStartRequest{
		SyncStamp: req.command.Common.Unknown[0], SourceID: req.command.Common.ObjectID,
		AbilityID: activeAbilityID, AbilityIndex: req.command.Ability.Index,
		SourceTime: req.packet.SourceTime, AnimationName: projected.AnimationName,
		MuzzleEffectName: projected.MuzzleEffectName,
		HitDelay:         projected.HitDelay, ReleaseDelay: projected.ReleaseDelay,
		Cooldown: projected.Cooldown,
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("healingTicksStart: %w", err)
	}
	manaPacket, err := abilityraknet.Mana(
		req.command.Common.ObjectID, remainingManaPoint,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("healingTicksManaPacket: %w", err)
	}
	cooldownReservation, isCooldownReserved :=
		peerSession.abilityCooldownSession().Reserve(
			zoneability.HeroAbilityCooldown(activeAbilityID),
			abilityStartTime, projected.Cooldown,
		)
	if !isCooldownReserved {
		r.registry.mutex.Unlock()
		return req.reject("healing ticks cooldown unavailable")
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			abilityStartTime, projected.ReleaseDelay,
		)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		r.registry.mutex.Unlock()
		return req.reject("healing ticks release unavailable")
	}
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("healingTicksCommit: %w", err)
	}
	run := &heroHealingTicksRun{}
	startPresentation := make([][]byte, 0, 2)
	if isFieldMedicSupport {
		effectSlot, isEffectAllocated := r.effectPool.Allocate(target.objectID)
		if !isEffectAllocated {
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			peerSession.abilityReleaseSession().Rollback(releaseReservation)
			_ = peerSession.setCampaignCharacterManaPoints(
				peerSession.deployedCreatureIndex, previousManaPoint,
			)
			r.registry.mutex.Unlock()
			return req.reject("field medic effect unavailable")
		}
		run.effectPool = r.effectPool
		run.effects = append(run.effects, fieldMedicEffectLease{
			objectID: target.objectID, slot: effectSlot,
		})
		effectPacket, marshalErr := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: effectSlot + 1, IsForceAttached: true,
			Asset: util.HashID(projected.HitEffectName), ObjectID: target.objectID,
		})
		if marshalErr != nil {
			run.ReleaseEffects()
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			peerSession.abilityReleaseSession().Rollback(releaseReservation)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("fieldMedicTargetEffect: %w", marshalErr)
		}
		startPresentation = append(startPresentation, effectPacket)
		if isFieldMedicAlly {
			beamSlot, isBeamAllocated := r.effectPool.Allocate(req.command.Common.ObjectID)
			if !isBeamAllocated {
				run.ReleaseEffects()
				peerSession.abilityCooldownSession().Rollback(cooldownReservation)
				peerSession.abilityReleaseSession().Rollback(releaseReservation)
				r.registry.mutex.Unlock()
				return req.reject("field medic beam unavailable")
			}
			run.effects = append(run.effects, fieldMedicEffectLease{
				objectID: req.command.Common.ObjectID, slot: beamSlot,
			})
			beamPacket, marshalErr := raknet.MarshalApplication(raknet.AttachedEffectMessage{
				Slot: beamSlot + 1, IsForceAttached: true,
				Asset:    util.HashID("effect_fieldmedicsupport_beam.ServerEventDef"),
				ObjectID: req.command.Common.ObjectID, SecondaryObjectID: target.objectID,
			})
			if marshalErr != nil {
				run.ReleaseEffects()
				peerSession.abilityCooldownSession().Rollback(cooldownReservation)
				peerSession.abilityReleaseSession().Rollback(releaseReservation)
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("fieldMedicBeamEffect: %w", marshalErr)
			}
			startPresentation = append(startPresentation, beamPacket)
		}
	}
	peerSession.heroHealingTicks = run
	generation := peerSession.generation
	creatureIndex := peerSession.deployedCreatureIndex
	binding := peerSession.binding
	position := raknet.Vector3(peerSession.playerPosition)
	if definition.Name == "FieldMedicSupport" {
		position = raknet.Vector3(target.position)
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	schedule := heroHealingTicksSchedule{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: req.command.Common.ObjectID,
		creatureIndex: creatureIndex, previousManaPoint: previousManaPoint,
		position: position, creature: creature, definition: projected,
		binding: binding, target: target, run: run,
		cooldownReservation: cooldownReservation,
		releaseReservation:  releaseReservation, releasePacket: start.Release,
		presentationPackets: startPresentation,
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, projected.NumberOfTicks+1)
	for tick := uint32(0); tick < projected.NumberOfTicks; tick++ {
		deadline := projected.HitDelay + time.Duration(tick)*projected.TickDuration
		step := heroHealingTicksStep{
			schedule: schedule, deadline: deadline, isFirst: tick == 0,
			isFinal: tick+1 == projected.NumberOfTicks,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: deadline, Produce: step.produce,
		})
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: projected.ReleaseDelay, Produce: schedule.release,
	})
	sortScheduledPacketProducersByDelay(producers)
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
		return nil, fmt.Errorf("healingTicksSchedule: %w", err)
	}
	run.mutex.Lock()
	run.cancel = cancel
	run.mutex.Unlock()
	r.logger.Printf(
		"RakNet hero healing ticks accepted ability=%s source=%d target=%d",
		projected.Name, req.command.Common.ObjectID, targetObjectID,
	)
	packets := append([][]byte{start.Acknowledge, manaPacket}, start.Presentation...)
	healthBuffPackets, healthBuffErr := r.applyFieldMedicSupportHealthBuff(
		req.packet, sessionKey, req.command.Common.ObjectID, target, projected,
	)
	if healthBuffErr != nil {
		r.logger.Printf(
			"RakNet Field Medic Support health buff omitted for %s: %v",
			sessionKey, healthBuffErr,
		)
	} else {
		packets = append(packets, healthBuffPackets...)
	}
	return packets, nil
}
