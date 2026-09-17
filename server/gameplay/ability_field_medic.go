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
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type fieldMedicActiveSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	sourceObjectID      uint32
	creatureIndex       uint32
	previousManaPoint   float32
	definition          sim.AbilityDefinition
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releasePacket       []byte
}

type fieldMedicTransferExpiry struct {
	runtime        campaignAbilityCommandRuntime
	sessionKey     string
	generation     uint64
	targetObjectID uint32
	expiresAt      time.Time
	run            *campaignNPCModifierRun
}

type fieldMedicDebuffCapture struct {
	winners   []zoneeffect.Modifier
	originals []zoneeffect.Modifier
}

func (e fieldMedicTransferExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.zone != nil &&
		peerSession.campaignNPCModifiers[e.run.instanceID] == e.run
	if isCurrent {
		clearFieldMedicNPCStatus(
			peerSession.zone.NPCs(), e.targetObjectID,
			e.run.record.GUID, e.expiresAt,
		)
		peerSession.zone.Effect().Remove(e.run.instanceID)
		peerSession.untrackCampaignNPCModifier(e.run)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	_, err := e.run.release(e.runtime.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("fieldMedicTransferRelease: %w", err)
	}
	packet, err := effectraknet.ModifierDelete(
		e.targetObjectID, e.run.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("fieldMedicTransferDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e fieldMedicActiveSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.zone != nil && peerSession.zone.Effect() != nil &&
		peerSession.deployedObjectID == e.sourceObjectID
}

func (e fieldMedicActiveSchedule) hit() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	center := game.Vec3(peerSession.playerPosition)
	debuffs := fieldMedicCapturedDebuffs(peerSession, center, e.definition.Radius)
	buffs := fieldMedicCapturedBuffs(peerSession, center, e.definition.Radius)
	packets := make([][]byte, 0)
	allyEffectTarget := make(map[uint32]bool)
	for _, modifier := range debuffs.originals {
		removed, removePackets, err := e.removeOriginalLocked(
			peerSession.zone, modifier,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("fieldMedicOriginal[%#x]: %w", modifier.GUID, err)
		}
		if !removed {
			continue
		}
		packets = append(packets, removePackets...)
		allyEffectTarget[modifier.TargetObjectID] = true
	}
	for _, modifier := range buffs.originals {
		removed, removePackets, err := e.removeEnemyBuffOriginalLocked(
			&peerSession, modifier,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("fieldMedicBuffOriginal[%#x]: %w", modifier.GUID, err)
		}
		if removed {
			packets = append(packets, removePackets...)
		}
	}
	peerSession = e.runtime.registry.sessions[e.sessionKey]
	transferredBuffs, buffTargets, err := e.transferBuffsLocked(
		&peerSession, center, buffs.winners,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, err
	}
	packets = append(packets, transferredBuffs...)
	for _, targetObjectID := range buffTargets {
		allyEffectTarget[targetObjectID] = true
	}
	for targetObjectID := range allyEffectTarget {
		packet, err := fieldMedicEffectPacket(
			e.definition.AllyEffectName, targetObjectID,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("fieldMedicAllyEffect: %w", err)
		}
		packets = append(packets, packet)
	}
	peerSession = e.runtime.registry.sessions[e.sessionKey]
	transferred, err := e.transferDebuffsLocked(
		&peerSession, center, debuffs.winners,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, err
	}
	packets = append(packets, transferred...)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	return packets, nil
}

func (e fieldMedicActiveSchedule) removeOriginalLocked(
	activeZone interface{ Effect() *zoneeffect.Inventory },
	modifier zoneeffect.Modifier,
) (bool, [][]byte, error) {
	for sessionKey, candidate := range e.runtime.registry.sessions {
		if candidate.zone == nil || candidate.zone.Effect() != activeZone.Effect() {
			continue
		}
		run := candidate.campaignNPCModifiers[modifier.InstanceID]
		if run == nil {
			continue
		}
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		clearFieldMedicHeroStatus(&candidate, modifier)
		candidate.zone.Effect().Remove(modifier.InstanceID)
		candidate.untrackCampaignNPCModifier(run)
		e.runtime.registry.sessions[sessionKey] = candidate
		_, err := run.release(e.runtime.modifierPool)
		if err != nil {
			return false, nil, fmt.Errorf("fieldMedicOriginalRelease: %w", err)
		}
		packet, err := effectraknet.ModifierDelete(
			modifier.TargetObjectID, modifier.InstanceID,
		)
		if err != nil {
			return false, nil, fmt.Errorf("fieldMedicOriginalDelete: %w", err)
		}
		return true, [][]byte{packet}, nil
	}
	return false, nil, nil
}

func (e fieldMedicActiveSchedule) transferDebuffsLocked(
	peerSession *gameplayPeerSession, center game.Vec3,
	captured []zoneeffect.Modifier,
) ([][]byte, error) {
	if peerSession == nil || peerSession.zone == nil ||
		peerSession.zone.NPCs() == nil {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for _, target := range peerSession.zone.NPCs().LiveSnapshots() {
		if target.Faction != zonenpc.FactionNonPlayerAligned ||
			target.Plan.IsFixture ||
			zonegeometry.Distance(center, target.Plan.Position) > e.definition.Radius {
			continue
		}
		isEffectAdded := false
		for _, modifier := range captured {
			transferPackets, isTransferred, err := e.transferDebuffLocked(
				peerSession, target.Plan.ObjectID, modifier,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"fieldMedicTransfer[%d/%#x]: %w",
					target.Plan.ObjectID, modifier.GUID, err,
				)
			}
			packets = append(packets, transferPackets...)
			isEffectAdded = isEffectAdded || isTransferred
		}
		if isEffectAdded {
			packet, err := fieldMedicEffectPacket(
				e.definition.EnemyEffectName, target.Plan.ObjectID,
			)
			if err != nil {
				return nil, fmt.Errorf("fieldMedicEnemyEffect: %w", err)
			}
			packets = append(packets, packet)
		}
	}
	return packets, nil
}

func (e fieldMedicActiveSchedule) transferDebuffLocked(
	peerSession *gameplayPeerSession, targetObjectID uint32,
	modifier zoneeffect.Modifier,
) ([][]byte, bool, error) {
	duration := min(modifier.Duration, e.definition.TimeToDestroyBuffs)
	if duration <= 0 || !isFieldMedicStatusSupported(modifier.GUID) {
		return nil, false, nil
	}
	expiresAt := e.runtime.now().Add(duration)
	err := applyFieldMedicNPCStatus(
		peerSession.zone.NPCs(), targetObjectID, modifier.GUID, expiresAt,
	)
	if err != nil {
		return nil, false, fmt.Errorf("fieldMedicStatusApply: %w", err)
	}
	run, err := newCampaignNPCModifierRun(e.runtime.modifierPool)
	if err != nil {
		clearFieldMedicNPCStatus(
			peerSession.zone.NPCs(), targetObjectID, modifier.GUID, expiresAt,
		)
		return nil, false, fmt.Errorf("fieldMedicRun: %w", err)
	}
	run.record = zoneeffect.Modifier{
		InstanceID: run.instanceID, GUID: modifier.GUID,
		SourceObjectID: e.sourceObjectID, TargetObjectID: targetObjectID,
		Rank: modifier.Rank, Duration: duration,
		Kind:            zoneeffect.ModifierKindDebuff,
		InitiatorObject: modifier.InitiatorObject,
	}
	err = peerSession.trackCampaignNPCModifier(run)
	if err == nil {
		err = peerSession.zone.Effect().Put(run.record)
	}
	if err != nil {
		peerSession.untrackCampaignNPCModifier(run)
		clearFieldMedicNPCStatus(
			peerSession.zone.NPCs(), targetObjectID, modifier.GUID, expiresAt,
		)
		_, _ = run.release(e.runtime.modifierPool)
		return nil, false, fmt.Errorf("fieldMedicTrack: %w", err)
	}
	packet, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: e.sourceObjectID, TargetObjectID: targetObjectID,
		ModifierID: modifier.GUID, InstanceID: run.instanceID,
		Duration: duration, Timestamp: e.packet.SourceTime +
			uint64(e.definition.HitDelay/time.Millisecond),
	})
	if err != nil {
		peerSession.zone.Effect().Remove(run.instanceID)
		peerSession.untrackCampaignNPCModifier(run)
		clearFieldMedicNPCStatus(
			peerSession.zone.NPCs(), targetObjectID, modifier.GUID, expiresAt,
		)
		_, _ = run.release(e.runtime.modifierPool)
		return nil, false, fmt.Errorf("fieldMedicCreate: %w", err)
	}
	expiry := fieldMedicTransferExpiry{
		runtime: e.runtime, sessionKey: e.sessionKey, generation: e.generation,
		targetObjectID: targetObjectID, expiresAt: expiresAt, run: run,
	}
	cancel, err := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: duration, Produce: expiry.produce,
	}})
	if err != nil {
		peerSession.zone.Effect().Remove(run.instanceID)
		peerSession.untrackCampaignNPCModifier(run)
		clearFieldMedicNPCStatus(
			peerSession.zone.NPCs(), targetObjectID, modifier.GUID, expiresAt,
		)
		_, _ = run.release(e.runtime.modifierPool)
		return nil, false, fmt.Errorf("fieldMedicSchedule: %w", err)
	}
	run.cancel = cancel
	if !run.create() {
		cancel()
		peerSession.zone.Effect().Remove(run.instanceID)
		peerSession.untrackCampaignNPCModifier(run)
		clearFieldMedicNPCStatus(
			peerSession.zone.NPCs(), targetObjectID, modifier.GUID, expiresAt,
		)
		_, _ = run.release(e.runtime.modifierPool)
		return nil, false, errors.New("fieldMedicCreateRun: modifier unavailable")
	}
	return [][]byte{packet}, true, nil
}

func (e fieldMedicActiveSchedule) release() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	animationPacket, err := abilityraknet.AnimationReset(
		e.sourceObjectID,
		e.packet.SourceTime+uint64(e.definition.ReleaseDelay/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("fieldMedicAnimationReset: %w", err)
	}
	return [][]byte{e.releasePacket, animationPacket}, nil
}

func (e fieldMedicActiveSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		_ = peerSession.setCampaignCharacterManaPoints(
			e.creatureIndex, e.previousManaPoint,
		)
		peerSession.abilityCooldownSession().Rollback(e.cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if isCurrent {
		e.runtime.logger.Printf(
			"RakNet Field Medic Active stopped after schedule failure for %s: %v",
			e.sessionKey, scheduleErr,
		)
	}
}

func (r campaignAbilityCommandRuntime) handleFieldMedicActive(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, ability zonecontent.HeroAbility,
	sessionKey string, abilityStartTime time.Time,
) ([][]byte, error) {
	definition := ability.Definition
	if ability.ID == 0 || definition.Name != "FieldMedicActive" ||
		definition.Kind != sim.AbilityKindModifierArea || definition.Radius <= 0 ||
		definition.TimeToDestroyBuffs <= 0 || definition.AnimationName == "" {
		r.registry.mutex.Unlock()
		return req.reject("Field Medic Active definition unavailable")
	}
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fieldMedicTiming: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(
		projected.ManaCost, creature.DamageProfile.PrimaryAttribute, projected.ManaCoefficient,
		peerSession.isOverdriveActiveAt(abilityStartTime),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fieldMedicMana: %w", err)
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}
	cooldown, err := zoneability.ProjectCooldown(creature, projected)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fieldMedicCooldown: %w", err)
	}
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp:    req.command.Common.Unknown[0],
		ResponseType: raknet.ActionResponseAccepted,
		ObjectID:     ability.ID, AbilityIndex: req.command.Ability.Index,
		SourceStartMilliseconds: req.packet.SourceTime,
		SourceCommitMilliseconds: req.packet.SourceTime +
			uint64(projected.HitDelay/time.Millisecond),
		SourceEndMilliseconds: req.packet.SourceTime +
			uint64(projected.ReleaseDelay/time.Millisecond),
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fieldMedicAck: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(
		req.command.Common.Unknown[0], ability.ID,
		req.command.Ability.Index, req.packet.SourceTime,
		projected.HitDelay, projected.ReleaseDelay,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fieldMedicRelease: %w", err)
	}
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	startPackets, err := abilityraknet.StartSpendPresentation(
		req.command.Common.ObjectID, ability.ID, projected.AnimationName,
		cooldown, req.packet.SourceTime, remainingManaPoint,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fieldMedicStart: %w", err)
	}
	cooldownReservation, isCooldownReserved :=
		peerSession.abilityCooldownSession().Reserve(
			zoneability.HeroAbilityCooldown(ability.ID), abilityStartTime, cooldown,
		)
	if !isCooldownReserved {
		r.registry.mutex.Unlock()
		return req.reject("Field Medic Active cooldown unavailable")
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			abilityStartTime, projected.ReleaseDelay,
		)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		r.registry.mutex.Unlock()
		return req.reject("Field Medic Active release unavailable")
	}
	previousManaPoint := peerSession.deployedManaPoint()
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fieldMedicCommit: %w", err)
	}
	schedule := fieldMedicActiveSchedule{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: peerSession.generation, sourceObjectID: req.command.Common.ObjectID,
		creatureIndex:     peerSession.deployedCreatureIndex,
		previousManaPoint: previousManaPoint, definition: projected,
		cooldownReservation: cooldownReservation,
		releaseReservation:  releaseReservation, releasePacket: releasePacket,
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	producers := r.registry.producerGuard.scheduledProducers(
		sessionKey, []raknet.ScheduledPacketProducer{
			{Delay: projected.HitDelay, Produce: schedule.hit},
			{Delay: projected.ReleaseDelay, Produce: schedule.release},
		},
	)
	if req.packet.ScheduleGroupResult != nil {
		_, err = req.packet.ScheduleGroupResult(producers, schedule.fail)
	} else if req.packet.ScheduleGroup != nil {
		_, err = req.packet.ScheduleGroup(producers)
	} else {
		err = errors.New("schedule unavailable")
	}
	if err != nil {
		schedule.fail(err)
		return nil, fmt.Errorf("fieldMedicSchedule: %w", err)
	}
	return append([][]byte{ackPacket}, startPackets...), nil
}
