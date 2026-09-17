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
)

type heroStatusAreaTarget struct {
	objectID   uint32
	instanceID uint32
	expiresAt  time.Time
	kind       sim.AbilityStatusKind
}

type heroStatusAreaRun struct {
	mutex        sync.Mutex
	npc          *zonenpc.Session
	modifierPool *modifierPool
	targets      []heroStatusAreaTarget
	cancel       raknet.CancelSchedule
	isCleaned    bool
}

func (e *heroStatusAreaRun) Add(target heroStatusAreaTarget) bool {
	if e == nil || target.objectID == 0 || target.instanceID == 0 ||
		target.expiresAt.IsZero() {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isCleaned {
		return false
	}
	e.targets = append(e.targets, target)
	return true
}

func (e *heroStatusAreaRun) Cleanup() []heroStatusAreaTarget {
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
	for _, target := range targets {
		switch target.kind {
		case sim.AbilityStatusKindBanish:
			e.npc.ClearBanish(target.objectID, target.expiresAt)
		case sim.AbilityStatusKindCurse:
			e.npc.ClearCurse(target.objectID, target.expiresAt)
		case sim.AbilityStatusKindSlow:
			e.npc.ClearSlow(target.objectID, target.expiresAt)
		case sim.AbilityStatusKindStun:
			e.npc.ClearStun(target.objectID, target.expiresAt)
		case sim.AbilityStatusKindSilence:
			e.npc.ClearSilence(target.objectID, target.expiresAt)
		case sim.AbilityStatusKindPhysicalVulnerability:
			e.npc.ClearPhysicalDamageVulnerability(
				target.objectID, target.expiresAt,
			)
		}
		_ = e.modifierPool.Release(target.instanceID)
	}
	return targets
}

func (r campaignAbilityCommandRuntime) applyAcceptedHitStatus(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, hitTimestamp uint64, plan zoneability.AreaPlan,
	results []zoneability.AreaResult,
) ([][]byte, error) {
	targetObjectIDs := make([]uint32, 0, len(results))
	for _, result := range results {
		if result.Damage.IsDamageImmune || result.Damage.IsDefeated ||
			result.Damage.Damage <= 0 {
			continue
		}
		targetObjectIDs = append(targetObjectIDs, result.Damage.ObjectID)
	}
	return r.applyTargetStatus(
		packet, sessionKey, generation, sourceObjectID, hitTimestamp,
		plan, targetObjectIDs,
	)
}

func (r campaignAbilityCommandRuntime) applyTargetStatus(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, hitTimestamp uint64, plan zoneability.AreaPlan,
	targetObjectIDs []uint32,
) ([][]byte, error) {
	definition := plan.Definition
	isSupported := definition.StatusKind == sim.AbilityStatusKindSlow ||
		definition.StatusKind == sim.AbilityStatusKindStun ||
		definition.StatusKind == sim.AbilityStatusKindSilence ||
		definition.StatusKind == sim.AbilityStatusKindPhysicalVulnerability
	if !isSupported || definition.StatusDuration <= 0 ||
		definition.RootModifierID == 0 {
		return nil, nil
	}
	run := &heroStatusAreaRun{modifierPool: r.modifierPool}
	messages := make([]raknet.ApplicationMessage, 0, len(targetObjectIDs))
	expiresAt := r.now().Add(definition.StatusDuration)
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation || peerSession.zone == nil {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	run.npc = peerSession.zone.NPCs()
	for index, targetObjectID := range targetObjectIDs {
		err := applyAcceptedHitNPCStatus(
			run.npc, targetObjectID, expiresAt, definition,
		)
		if err != nil {
			r.registry.mutex.Unlock()
			run.Stop()
			return nil, fmt.Errorf("hitStatusApply[%d]: %w", index, err)
		}
		instanceID, err := r.modifierPool.Allocate()
		if err != nil {
			r.registry.mutex.Unlock()
			run.Stop()
			return nil, fmt.Errorf("hitStatusModifier[%d]: %w", index, err)
		}
		target := heroStatusAreaTarget{
			objectID: targetObjectID, instanceID: instanceID,
			expiresAt: expiresAt, kind: definition.StatusKind,
		}
		if !run.Add(target) {
			_ = r.modifierPool.Release(instanceID)
			clearAcceptedHitNPCStatus(
				run.npc, targetObjectID, expiresAt, definition.StatusKind,
			)
			continue
		}
		messages = append(messages, raknet.ModifierCreatedMessage{
			TargetID: targetObjectID, ModifierGUID: definition.RootModifierID,
			InstanceID:           instanceID,
			DurationMilliseconds: uint32(definition.StatusDuration.Milliseconds()),
			StackCount:           1, StartMilliseconds: hitTimestamp, SourceID: sourceObjectID,
		})
	}
	if len(messages) == 0 {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.heroStatusAreas == nil {
		peerSession.heroStatusAreas = make(map[uint32]*heroStatusAreaRun)
	}
	previous := peerSession.heroStatusAreas[plan.AbilityID]
	peerSession.heroStatusAreas[plan.AbilityID] = run
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	if previous != nil {
		previous.Stop()
	}

	schedule := heroStatusAreaSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: sourceObjectID,
		abilityID: plan.AbilityID, definition: definition, run: run,
	}
	producers := []raknet.ScheduledPacketProducer{{
		Delay: definition.StatusDuration, Produce: schedule.expire,
	}}
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	cancel, err := packet.ScheduleProducers(producers)
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		r.stopAcceptedHitStatus(sessionKey, generation, plan.AbilityID, run)
		return nil, fmt.Errorf("hitStatusSchedule: %w", err)
	}
	run.mutex.Lock()
	run.cancel = cancel
	run.mutex.Unlock()

	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		modifierPacket, err := raknet.MarshalApplication(message)
		if err != nil {
			r.stopAcceptedHitStatus(sessionKey, generation, plan.AbilityID, run)
			return nil, fmt.Errorf("hitStatusMarshal[%d]: %w", index, err)
		}
		packets = append(packets, modifierPacket)
	}
	return packets, nil
}

func applyAcceptedHitNPCStatus(
	npc *zonenpc.Session, objectID uint32, expiresAt time.Time,
	definition sim.AbilityDefinition,
) error {
	switch definition.StatusKind {
	case sim.AbilityStatusKindSlow:
		return npc.ApplySlow(
			objectID, expiresAt,
			definition.StatusMovementScale, definition.StatusAttackScale,
		)
	case sim.AbilityStatusKindStun:
		return npc.ApplyStun(objectID, expiresAt)
	case sim.AbilityStatusKindSilence:
		return npc.ApplySilence(objectID, expiresAt)
	case sim.AbilityStatusKindPhysicalVulnerability:
		return npc.ApplyPhysicalDamageVulnerability(
			objectID, definition.StatusPhysicalIncrease, expiresAt,
		)
	default:
		return errors.New("unsupported accepted-hit status")
	}
}

func clearAcceptedHitNPCStatus(
	npc *zonenpc.Session, objectID uint32, expiresAt time.Time,
	statusKind sim.AbilityStatusKind,
) {
	switch statusKind {
	case sim.AbilityStatusKindSlow:
		npc.ClearSlow(objectID, expiresAt)
	case sim.AbilityStatusKindStun:
		npc.ClearStun(objectID, expiresAt)
	case sim.AbilityStatusKindSilence:
		npc.ClearSilence(objectID, expiresAt)
	case sim.AbilityStatusKindPhysicalVulnerability:
		npc.ClearPhysicalDamageVulnerability(objectID, expiresAt)
	}
}

func (r campaignAbilityCommandRuntime) stopAcceptedHitStatus(
	sessionKey string, generation uint64, abilityID uint32,
	run *heroStatusAreaRun,
) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.heroStatusAreas[abilityID] == run
	if isCurrent {
		delete(peerSession.heroStatusAreas, abilityID)
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	if isCurrent {
		run.Stop()
	}
}

func (e *heroStatusAreaRun) Stop() {
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
}

type heroStatusAreaSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	sourceObjectID      uint32
	abilityID           uint32
	creatureIndex       uint32
	previousManaPoint   float32
	center              raknet.Vector3
	definition          sim.AbilityDefinition
	run                 *heroStatusAreaRun
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releasePacket       []byte
}

func (e heroStatusAreaSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.heroStatusAreas[e.abilityID] == e.run
}

func (e heroStatusAreaSchedule) apply() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	expiresAt := e.runtime.now().Add(e.definition.Duration)
	targets := make([]zonenpc.Snapshot, 0)
	for _, target := range peerSession.zone.NPCs().LiveSnapshots() {
		if target.Faction != zonenpc.FactionNonPlayerAligned ||
			zonegeometry.Distance(game.Vec3(e.center), target.Plan.Position) >
				e.definition.Radius {
			continue
		}
		targets = append(targets, target)
	}
	messages := make([]raknet.ApplicationMessage, 0, len(targets)+1)
	messages = append(messages, raknet.DropPresentationMessage{
		Asset: util.HashID(e.definition.ActivationEffectName), Position: e.center,
	})
	for index, target := range targets {
		var err error
		switch e.definition.StatusKind {
		case sim.AbilityStatusKindBanish:
			err = peerSession.zone.NPCs().ApplyBanish(target.Plan.ObjectID, expiresAt)
		case sim.AbilityStatusKindCurse:
			err = peerSession.zone.NPCs().ApplyCurseProfile(
				target.Plan.ObjectID, expiresAt, zonenpc.CurseDamageProfile{
					DamageBuff:       e.definition.StatusDamageBuff,
					PhysicalIncrease: e.definition.StatusPhysicalIncrease,
					EnergyIncrease:   e.definition.StatusEnergyIncrease,
				},
			)
		default:
			err = errors.New("unsupported status kind")
		}
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("statusAreaBanish[%d]: %w", index, err)
		}
		remaining := time.Duration(0)
		switch e.definition.StatusKind {
		case sim.AbilityStatusKindBanish:
			remaining = peerSession.zone.NPCs().BanishRemaining(
				target.Plan.ObjectID, e.runtime.now(),
			)
		case sim.AbilityStatusKindCurse:
			remaining = peerSession.zone.NPCs().CurseRemaining(
				target.Plan.ObjectID, e.runtime.now(),
			)
		}
		if remaining == 0 {
			continue
		}
		instanceID, allocateErr := e.runtime.modifierPool.Allocate()
		if allocateErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("statusAreaModifier[%d]: %w", index, allocateErr)
		}
		tracked := heroStatusAreaTarget{
			objectID: target.Plan.ObjectID, instanceID: instanceID,
			expiresAt: expiresAt, kind: e.definition.StatusKind,
		}
		if !e.run.Add(tracked) {
			_ = e.runtime.modifierPool.Release(instanceID)
			switch e.definition.StatusKind {
			case sim.AbilityStatusKindBanish:
				peerSession.zone.NPCs().ClearBanish(target.Plan.ObjectID, expiresAt)
			case sim.AbilityStatusKindCurse:
				peerSession.zone.NPCs().ClearCurse(target.Plan.ObjectID, expiresAt)
			}
			continue
		}
		messages = append(messages, raknet.ModifierCreatedMessage{
			TargetID:             target.Plan.ObjectID,
			ModifierGUID:         e.definition.RootModifierID,
			InstanceID:           instanceID,
			DurationMilliseconds: uint32(e.definition.Duration.Milliseconds()),
			StackCount:           1, StartMilliseconds: e.packet.SourceTime +
				uint64(e.definition.HitDelay/time.Millisecond),
			SourceID: e.sourceObjectID,
		})
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("statusAreaApplyMarshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e heroStatusAreaSchedule) release() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.releasePacket}, nil
}

func (e heroStatusAreaSchedule) expire() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		delete(peerSession.heroStatusAreas, e.abilityID)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	targets := e.run.Cleanup()
	if !isCurrent || len(targets) == 0 {
		return nil, nil
	}
	packets := make([][]byte, 0, len(targets))
	for index, target := range targets {
		packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{
			TargetID: target.objectID, InstanceID: target.instanceID,
		})
		if err != nil {
			return nil, fmt.Errorf("statusAreaDelete[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e heroStatusAreaSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		delete(peerSession.heroStatusAreas, e.abilityID)
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
		"RakNet hero status area stopped after schedule failure for %s: %v",
		e.sessionKey, scheduleErr,
	)
}

func (r campaignAbilityCommandRuntime) handleHeroStatusArea(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	activeAbilityID uint32, sessionKey string, abilityStartTime time.Time,
) ([][]byte, error) {
	isStatusSupported := definition.StatusKind == sim.AbilityStatusKindBanish ||
		definition.StatusKind == sim.AbilityStatusKindCurse
	if activeAbilityID == 0 || definition.Kind != sim.AbilityKindStatusArea ||
		!isStatusSupported ||
		definition.Range <= 0 || definition.Radius <= 0 ||
		definition.Duration <= 0 || definition.RootModifierID == 0 ||
		definition.AnimationName == "" || definition.ActivationEffectName == "" {
		r.registry.mutex.Unlock()
		return req.reject("status area definition unavailable")
	}
	center := req.command.Ability.TargetPosition
	if !isReportedZonePosition(center) {
		center = req.command.Ability.CursorPosition
	}
	admissionRange := heroAbilityAdmissionRange(creature, definition)
	if !isReportedZonePosition(center) || !isFiniteZonePosition(center) ||
		!isInsideZoneTrigger(peerSession.playerPosition, center, admissionRange) {
		r.registry.mutex.Unlock()
		return req.reject("status area position unavailable")
	}
	if peerSession.heroStatusAreas[activeAbilityID] != nil {
		r.registry.mutex.Unlock()
		return req.reject("status area already active")
	}
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("statusAreaTiming: %w", err)
	}
	if projected.IsAreaDurationScaled {
		projected.NumberOfTicks, err = game.ResolveAreaDurationCount(
			projected.NumberOfTicks, creature.AreaDurationIncrease,
		)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("statusAreaDuration: %w", err)
		}
	}
	manaCost, err := game.ResolveAbilityManaCost(
		projected.ManaCost, creature.DamageProfile.PrimaryAttribute,
		projected.ManaCoefficient, peerSession.isOverdriveActiveAt(abilityStartTime),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("statusAreaMana: %w", err)
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
		return nil, fmt.Errorf("statusAreaStart: %w", err)
	}
	manaPacket, err := abilityraknet.Mana(
		req.command.Common.ObjectID, remainingManaPoint,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("statusAreaManaPacket: %w", err)
	}
	cooldownReservation, isCooldownReserved :=
		peerSession.abilityCooldownSession().Reserve(
			zoneability.HeroAbilityCooldown(activeAbilityID),
			abilityStartTime, projected.Cooldown,
		)
	if !isCooldownReserved {
		r.registry.mutex.Unlock()
		return req.reject("status area cooldown unavailable")
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			abilityStartTime, projected.ReleaseDelay,
		)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		r.registry.mutex.Unlock()
		return req.reject("status area release unavailable")
	}
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("statusAreaCommit: %w", err)
	}
	run := &heroStatusAreaRun{
		npc: peerSession.zone.NPCs(), modifierPool: r.modifierPool,
	}
	if peerSession.heroStatusAreas == nil {
		peerSession.heroStatusAreas = make(map[uint32]*heroStatusAreaRun)
	}
	peerSession.heroStatusAreas[activeAbilityID] = run
	generation := peerSession.generation
	creatureIndex := peerSession.deployedCreatureIndex
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	schedule := heroStatusAreaSchedule{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: req.command.Common.ObjectID,
		abilityID: activeAbilityID, creatureIndex: creatureIndex,
		previousManaPoint: previousManaPoint, center: center,
		definition: projected, run: run,
		cooldownReservation: cooldownReservation,
		releaseReservation:  releaseReservation, releasePacket: start.Release,
	}
	tickCount := max(uint32(1), projected.NumberOfTicks)
	producers := make([]raknet.ScheduledPacketProducer, 0, tickCount+2)
	for tick := uint32(0); tick < tickCount; tick++ {
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay:   projected.HitDelay + time.Duration(tick)*projected.TickDuration,
			Produce: schedule.apply,
		})
	}
	producers = append(producers,
		raknet.ScheduledPacketProducer{
			Delay: projected.ReleaseDelay, Produce: schedule.release,
		},
		raknet.ScheduledPacketProducer{
			Delay: projected.HitDelay + time.Duration(tickCount-1)*
				projected.TickDuration + projected.Duration,
			Produce: schedule.expire,
		},
	)
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
		return nil, fmt.Errorf("statusAreaSchedule: %w", err)
	}
	run.mutex.Lock()
	run.cancel = cancel
	run.mutex.Unlock()
	r.logger.Printf(
		"RakNet hero status area accepted ability=%s source=%d center=(%g,%g,%g)",
		projected.Name, req.command.Common.ObjectID, center.X, center.Y, center.Z,
	)
	packets := append([][]byte{start.Acknowledge, manaPacket}, start.Presentation...)
	return packets, nil
}
