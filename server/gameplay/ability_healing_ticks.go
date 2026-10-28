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
	mutex          sync.Mutex
	cancel         raknet.CancelSchedule
	effects        *channelEffects
	isStopped      bool
	isCommitted    bool
	isChanneled    bool
	sourceObjectID uint32
	sourceTime     uint64
	startedAt      time.Time
	releasePacket  []byte
}

func (e *heroHealingTicksRun) Stop() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	cancel := e.cancel
	e.cancel = nil
	e.isStopped = true
	e.effects.Retire()
	e.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (e *heroHealingTicksRun) retireEffects() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.effects.Retire()
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
	if !isFound || peerSession.generation != e.generation ||
		peerSession.heroHealingTicks != e.run ||
		peerSession.deployedObjectID != e.sourceObjectID {
		return false
	}
	e.run.mutex.Lock()
	defer e.run.mutex.Unlock()
	return !e.run.isStopped
}

func (e heroHealingTicksSchedule) stopLocked(
	peerSession gameplayPeerSession,
) raknet.CancelSchedule {
	peerSession.heroHealingTicks = nil
	e.run.mutex.Lock()
	cancel := e.run.cancel
	e.run.cancel = nil
	e.run.isStopped = true
	e.run.effects.Retire()
	e.run.mutex.Unlock()
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.commitChannelRemovalsLocked()
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
	e.run.retireEffects()
	return packets
}

func (e heroHealingTicksSchedule) tick(
	deadline time.Duration, isFirst bool, isFinal bool,
) ([][]byte, error) {
	if e.definition.Name == "Sporogenesis" {
		if !isFirst {
			return nil, nil
		}
		e.commitAdmission()
		return e.startSporogenesis(deadline)
	}
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	// A tick can run before the acceptance response reaches transport commit.
	// Once production starts, failure must not refund an accepted cast.
	e.run.mutex.Lock()
	e.run.isCommitted = true
	e.run.mutex.Unlock()
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
	if e.definition.Name == "FieldMedicSupport" &&
		(!isActivePartyRecipient(targetSession, peerSession) ||
			targetSession.generation != e.target.generation ||
			(!e.target.isCompanion &&
				(targetSession.deployedObjectID != e.target.objectID ||
					targetSession.deployedCreatureIndex != e.target.creatureIndex))) {
		cancel := e.stopLocked(peerSession)
		e.runtime.registry.mutex.Unlock()
		return e.stopPackets(cancel, deadline), nil
	}
	hitPointBefore := float32(0)
	maximumHitPoint := float32(0)
	healingTargetProfile := game.HealingTargetProfile{}
	if e.target.isCompanion {
		companion, isCompanionFound :=
			targetSession.zone.Companion().Snapshot(e.target.objectID)
		if !isCompanionFound || !isPartyCompanionOwner(companion, targetSession) ||
			!companion.IsTargetable || companion.HitPoint <= 0 {
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
	packets, err := e.healingPacketsLocked(targetSession, hitPoint, healedAmount, isFirst, isFinal)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("healingTicksFeedback: %w", err)
	}
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
		if !e.target.isCompanion && targetSession.zone != nil {
			targetSession.zone.PublishHeroResourceTo(
				e.target.userID, e.target.generation,
			)
		}
		e.runtime.registry.sessions[e.target.sessionKey] = targetSession
	} else {
		peerSession = targetSession
	}
	if isFinal && !e.run.isChanneled {
		peerSession.heroHealingTicks = nil
		e.run.mutex.Lock()
		e.run.cancel = nil
		e.run.mutex.Unlock()
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	if isFirst && e.run.isChanneled {
		err = e.runtime.registry.publishChannelEffectsLocked(e.run.effects, e.presentationPackets)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			e.fail(fmt.Errorf("healingEffectsPublish: %w", err))
			return packets, nil
		}
	}
	if peerSession.binding.Mode == game.ModeArena {
		match := e.runtime.registry.arenaMatches[peerSession.binding.GameID]
		if match != nil && match.isCombatActive {
			match.result.recordHealing(e.binding.UserID, targetSession.binding.UserID, healedAmount)
		}
		if healedAmount > 0 && e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena shared healing tick ability=%s source=%d target=%d amount=%g final=%t",
				e.definition.Name, e.sourceObjectID, e.target.objectID, healedAmount, isFinal)
		}
		e.runtime.registry.queueArenaAbilityResourcesLocked(peerSession, packets)
	}
	e.runtime.registry.mutex.Unlock()

	if healedAmount <= 0 || peerSession.binding.Mode == game.ModeArena {
		return packets, nil
	}
	err = e.runtime.stats.Record(
		context.Background(), e.binding,
		sporenet.PlayerStatDelta{
			PVEHealing: float64(healedAmount),
		},
	)
	if err != nil {
		e.runtime.logger.Printf("RakNet accepted healing source stats failed: %v", err)
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
		e.runtime.logger.Printf("RakNet accepted healing target stats failed: %v", err)
	}
	return packets, nil
}

// Prepare feedback from the same target snapshot before committing its heal.
func (e heroHealingTicksSchedule) healingPacketsLocked(
	targetSession gameplayPeerSession, hitPoint float32, healedAmount float32,
	isFirst bool, isFinal bool,
) ([][]byte, error) {
	packets := make([][]byte, 0, 5)
	if isFirst && !e.run.isChanneled {
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
	if healedAmount <= 0 {
		return packets, nil
	}
	healingDefinition := e.definition
	if healingDefinition.Name == "FieldMedicSupport" {
		// Reconstruct's target effect is a looping attached effect owned by the
		// run. Re-emitting it as a positioned effect on every tick creates
		// untracked visual and audio loops that cannot be stopped at release.
		healingDefinition.HealEffectName = ""
	}
	healingPackets, err := abilityraknet.MarshalTreeOfLifeHealing(
		healingDefinition, e.position,
		[]abilityraknet.Healing{{
			SourceObjectID: e.sourceObjectID, ObjectID: e.target.objectID,
			HitPoint: hitPoint, Amount: healedAmount,
		}}, isFinal,
	)
	if err != nil {
		return nil, fmt.Errorf("healingTicksMarshal: %w", err)
	}
	resourcePacket, err := e.resourcePacketLocked(targetSession, hitPoint)
	if err != nil {
		return nil, fmt.Errorf("healingTicksResource: %w", err)
	}
	packets = append(packets, healingPackets...)
	packets = append(packets, resourcePacket)
	return packets, nil
}

func (e heroHealingTicksSchedule) resourcePacketLocked(
	targetSession gameplayPeerSession, hitPoint float32,
) ([]byte, error) {
	var packet []byte
	var err error
	if e.target.isCompanion {
		packet, err = raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
			ObjectID: e.target.objectID, HitPoints: hitPoint,
			IsHitPointChanged: true,
		})
	} else {
		packet, err = targetSession.marshalCampaignCharacterResourceAt(
			e.target.creatureIndex, hitPoint,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("resourceMarshal: %w", err)
	}
	return packet, nil
}

func (e heroHealingTicksSchedule) release() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation
	if e.run.isChanneled {
		isCurrent = e.isCurrent(peerSession, isFound)
		if isCurrent {
			peerSession.heroHealingTicks = nil
			e.run.mutex.Lock()
			e.run.isStopped = true
			e.run.cancel = nil
			e.run.effects.Retire()
			e.run.mutex.Unlock()
			e.runtime.registry.sessions[e.sessionKey] = peerSession
			e.runtime.registry.commitChannelRemovalsLocked()
		}
	}
	e.runtime.registry.mutex.Unlock()
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
		e.run.mutex.Lock()
		isCommitted := e.run.isCommitted
		e.run.mutex.Unlock()
		if !isCommitted {
			manaErr := peerSession.setCampaignCharacterManaPoints(
				e.creatureIndex, e.previousManaPoint,
			)
			if manaErr != nil {
				e.runtime.logger.Printf("RakNet healing ticks mana rollback failed: %v", manaErr)
			}
			peerSession.abilityCooldownSession().Rollback(e.cooldownReservation)
			peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
		}
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	e.run.Stop()
	e.runtime.registry.mutex.Lock()
	e.runtime.registry.commitChannelRemovalsLocked()
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return
	}
	e.runtime.logger.Printf(
		"RakNet hero healing ticks stopped after schedule failure for %s: %v",
		e.sessionKey, scheduleErr,
	)
}

func (e heroHealingTicksSchedule) commitAdmission() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		return
	}
	e.run.mutex.Lock()
	e.run.isCommitted = true
	e.run.mutex.Unlock()
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
	var projected sim.AbilityDefinition
	var err error
	if definition.Name == "FieldMedicSupport" {
		projected, err = zoneability.ProjectHealingChannelTiming(creature, definition)
	} else {
		projected, err = zoneability.ProjectTiming(creature, definition)
	}
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
	channelDuration := time.Duration(0)
	if isFieldMedicSupport {
		channelDuration = projected.Duration
	}
	start, err := abilityraknet.StartAreaBasic(abilityraknet.AreaBasicStartRequest{
		SyncStamp: req.command.Common.Unknown[0], SourceID: req.command.Common.ObjectID,
		AbilityID: activeAbilityID, AbilityIndex: req.command.Ability.Index,
		SourceTime: req.packet.SourceTime, AnimationName: projected.AnimationName,
		MuzzleEffectName: projected.MuzzleEffectName,
		HitDelay:         projected.HitDelay, ReleaseDelay: projected.ReleaseDelay,
		Cooldown: projected.Cooldown, ChannelDuration: channelDuration,
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
	run := &heroHealingTicksRun{
		isChanneled: isFieldMedicSupport, sourceObjectID: req.command.Common.ObjectID,
		sourceTime: req.packet.SourceTime, startedAt: abilityStartTime,
		releasePacket: start.Release,
	}
	startPresentation := make([][]byte, 0, 2)
	if isFieldMedicSupport {
		objectIDs := []uint32{target.objectID}
		if isFieldMedicAlly {
			objectIDs = append(objectIDs, req.command.Common.ObjectID)
		}
		run.effects, err = r.registry.allocateAbilityChannelEffectsLocked(peerSession, r.effectPool, objectIDs)
		if err != nil {
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			peerSession.abilityReleaseSession().Rollback(releaseReservation)
			manaErr := peerSession.setCampaignCharacterManaPoints(
				peerSession.deployedCreatureIndex, previousManaPoint,
			)
			if manaErr != nil {
				r.logger.Printf("RakNet healing attachment admission mana rollback failed: %v", manaErr)
			}
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("healingEffectsAllocate: %w", err)
		}
		peerSession.channelRegistry = r.registry
		slots := run.effects.Slots()
		effectPacket, marshalErr := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: slots[0].slot + 1, IsForceAttached: true,
			Asset: util.HashID(projected.HitEffectName), ObjectID: target.objectID,
		})
		if marshalErr != nil {
			run.retireEffects()
			r.registry.commitChannelRemovalsLocked()
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			peerSession.abilityReleaseSession().Rollback(releaseReservation)
			manaErr := peerSession.setCampaignCharacterManaPoints(
				peerSession.deployedCreatureIndex, previousManaPoint,
			)
			if manaErr != nil {
				r.logger.Printf("RakNet healing target effect mana rollback failed: %v", manaErr)
			}
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("fieldMedicTargetEffect: %w", marshalErr)
		}
		startPresentation = append(startPresentation, effectPacket)
		if isFieldMedicAlly {
			beamPacket, marshalErr := raknet.MarshalApplication(raknet.AttachedEffectMessage{
				Slot: slots[1].slot + 1, IsForceAttached: true,
				Asset:    util.HashID("effect_fieldmedicsupport_beam.ServerEventDef"),
				ObjectID: req.command.Common.ObjectID, SecondaryObjectID: target.objectID,
			})
			if marshalErr != nil {
				run.retireEffects()
				r.registry.commitChannelRemovalsLocked()
				peerSession.abilityCooldownSession().Rollback(cooldownReservation)
				peerSession.abilityReleaseSession().Rollback(releaseReservation)
				manaErr := peerSession.setCampaignCharacterManaPoints(
					peerSession.deployedCreatureIndex, previousManaPoint,
				)
				if manaErr != nil {
					r.logger.Printf("RakNet healing beam effect mana rollback failed: %v", manaErr)
				}
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
	err = req.packet.AfterResponseCommit(schedule.commitAdmission)
	if err != nil {
		schedule.fail(err)
		return nil, fmt.Errorf("healingTicksAcceptance: %w", err)
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
	if err == nil && cancel == nil {
		err = errors.New("schedule cancellation unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		schedule.fail(err)
		return nil, fmt.Errorf("healingTicksSchedule: %w", err)
	}
	run.mutex.Lock()
	isStopped := run.isStopped
	if !isStopped {
		run.cancel = cancel
	}
	run.mutex.Unlock()
	if isStopped && cancel != nil {
		cancel()
	}
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
