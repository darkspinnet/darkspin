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
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type webTransfusionEffect struct {
	sourceSlot uint8
	targetSlot uint8
}

type webTransfusionRun struct {
	effects            map[uint32]webTransfusionEffect
	isRootPublished    bool
	relaxAnimationName string
	startedAt          time.Time
}

type webTransfusionTickStep struct {
	schedule heroDrainSchedule
	deadline time.Duration
}

func (e webTransfusionTickStep) produce() ([][]byte, error) {
	return e.schedule.tickWebTransfusion(e.deadline)
}

// Registry and run locks are held. Queue removals before returning effect slots
// so a replacement action cannot reuse a slot ahead of its removal packet.
func (e *heroDrainRun) retireWebTransfusionLocked() error {
	packets := make([][]byte, 0, len(e.areaDrain.effects)*2+1)
	if e.areaDrain.isRootPublished {
		removals, err := abilityraknet.ChannelDrainTargetEffectRemoval(e.sourceObjectID, e.sourceEffectSlot)
		if err != nil {
			return fmt.Errorf("webRootRemove: %w", err)
		}
		packets = append(packets, removals...)
	}
	for targetID, effect := range e.areaDrain.effects {
		removals, err := abilityraknet.ChannelDrainEffectRemovals(e.sourceObjectID, effect.sourceSlot, targetID, effect.targetSlot)
		if err != nil {
			return fmt.Errorf("webPairRemove: %w", err)
		}
		packets = append(packets, removals...)
	}
	e.queueRemovalsLocked(packets)
	e.isStopped = true
	e.isEffectsReleased = true
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	e.effectPool.Release(e.sourceObjectID, e.sourceEffectSlot)
	for targetID, effect := range e.areaDrain.effects {
		e.effectPool.Release(e.sourceObjectID, effect.sourceSlot)
		e.effectPool.Release(targetID, effect.targetSlot)
	}
	e.areaDrain.effects = nil
	return nil
}

func (e heroDrainSchedule) refreshWebTransfusionEffects(targets []zonenpc.Snapshot) error {
	e.run.mutex.Lock()
	defer e.run.mutex.Unlock()
	if e.run.isStopped {
		return nil
	}
	if !e.run.areaDrain.isRootPublished {
		rootPacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: e.run.sourceEffectSlot + 1, IsForceAttached: true,
			ObjectID: e.sourceObjectID, Asset: util.HashID(e.definition.ActivationEffectName),
		})
		if err != nil {
			return fmt.Errorf("webRootAttach: %w", err)
		}
		e.run.queuePresentationLocked([][]byte{rootPacket})
		e.run.areaDrain.isRootPublished = true
	}
	targetIDs := make(map[uint32]struct{}, len(targets))
	for _, target := range targets {
		targetIDs[target.Plan.ObjectID] = struct{}{}
	}
	for targetID, effect := range e.run.areaDrain.effects {
		_, isPresent := targetIDs[targetID]
		if isPresent {
			continue
		}
		packets, err := abilityraknet.ChannelDrainEffectRemovals(e.sourceObjectID, effect.sourceSlot, targetID, effect.targetSlot)
		if err != nil {
			return fmt.Errorf("webDepartRemove: %w", err)
		}
		e.run.queueRemovalsLocked(packets)
		e.run.effectPool.Release(e.sourceObjectID, effect.sourceSlot)
		e.run.effectPool.Release(targetID, effect.targetSlot)
		delete(e.run.areaDrain.effects, targetID)
	}
	for _, target := range targets {
		targetID := target.Plan.ObjectID
		_, isAttached := e.run.areaDrain.effects[targetID]
		if isAttached {
			effect := e.run.areaDrain.effects[targetID]
			packets, err := abilityraknet.ChannelDrainTargetEffect(e.definition.HitEffectName, e.sourceObjectID, targetID, effect.targetSlot)
			if err != nil {
				return fmt.Errorf("webSparkRefresh: %w", err)
			}
			e.run.queuePresentationLocked(packets)
			continue
		}
		sourceSlot, isSourceAllocated := e.run.effectPool.Allocate(e.sourceObjectID)
		if !isSourceAllocated {
			e.runtime.logger.Printf("RakNet Web Transfusion beam capacity reached source=%d target=%d", e.sourceObjectID, targetID)
			continue
		}
		targetSlot, isTargetAllocated := e.run.effectPool.Allocate(targetID)
		if !isTargetAllocated {
			e.run.effectPool.Release(e.sourceObjectID, sourceSlot)
			e.runtime.logger.Printf("RakNet Web Transfusion spark capacity reached target=%d", targetID)
			continue
		}
		packets, err := abilityraknet.ChannelDrainAttachedEffects(e.definition.MuzzleEffectName, e.definition.HitEffectName, e.sourceObjectID, sourceSlot, targetID, targetSlot)
		if err != nil {
			e.run.effectPool.Release(e.sourceObjectID, sourceSlot)
			e.run.effectPool.Release(targetID, targetSlot)
			return fmt.Errorf("webPairAttach: %w", err)
		}
		e.run.areaDrain.effects[targetID] = webTransfusionEffect{sourceSlot: sourceSlot, targetSlot: targetSlot}
		e.run.queuePresentationLocked(packets)
	}
	return nil
}

func (e heroDrainSchedule) tickWebTransfusion(deadline time.Duration) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	e.run.cast.activateLocked(e.run.cast.revision)
	radius, err := zoneability.ProjectAreaRadius(e.creature, e.definition.Radius)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("webPulseRadius: %w", err)
	}
	if peerSession.binding.Mode == game.ModeArena {
		return e.tickArenaWebTransfusionLocked(peerSession, radius, deadline)
	}
	targets := make([]zonenpc.Snapshot, 0)
	for _, target := range peerSession.zone.NPCs().LiveSnapshots() {
		if target.Faction == zonenpc.FactionNonPlayerAligned &&
			zonegeometry.Distance(game.Vec3(peerSession.playerPosition), target.Plan.Position) <= radius {
			targets = append(targets, target)
		}
	}
	err = e.refreshWebTransfusionEffects(targets)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("webPulseEffects: %w", err)
	}
	peerSession = e.runtime.registry.sessions[e.sessionKey]
	damage, err := zoneability.ProjectDamage(e.creature, e.definition, e.definition.MinimumDamage, e.definition.MaximumDamage)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("webPulseProject: %w", err)
	}
	// Persistent sparks are owned by the channel; do not replay a one-shot hit
	// effect through the damage publisher on top of the attached spark.
	definition := e.definition
	definition.HitEffectName = ""
	results, err := zoneability.CommitArea(peerSession.zone.Population().Random(), peerSession.zone.NPCs(), zoneability.AreaPlan{
		SourceObjectID: e.sourceObjectID, AbilityID: util.HashID(definition.Name),
		Definition: definition, Damage: damage, Target: targets, Center: game.Vec3(peerSession.playerPosition),
	}, e.creature, peerSession.binding.Difficulty, e.runtime.program.Critical)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("webPulseCommit: %w", err)
	}
	transitions := make([]campaignDamageTransition, 0, len(results))
	damageAmount := float32(0)
	for _, result := range results {
		transition, transitionErr := peerSession.applyCampaignDamageTransition(result.Damage)
		if transitionErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("webPulseTransition: %w", transitionErr)
		}
		transitions = append(transitions, transition)
		damageAmount += result.Damage.Damage
	}
	healingPackets, err := e.healWebTransfusion(&peerSession, damageAmount)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("webPulseHealing: %w", err)
	}
	packets, err := e.runtime.damage.publishAreaResults(e.packet, e.sessionKey, e.generation, e.sourceObjectID,
		e.packet.SourceTime+uint64(deadline/time.Millisecond), e.binding, results, transitions, nil, false)
	if err != nil {
		return nil, fmt.Errorf("webPulsePublish: %w", err)
	}
	return append(packets, healingPackets...), nil
}

func (e heroDrainSchedule) healWebTransfusion(peerSession *gameplayPeerSession, damage float32) ([][]byte, error) {
	if damage <= 0 {
		return nil, nil
	}
	healing, err := game.ApplyTargetHealingReduction(damage*e.definition.LifeSteal, peerSession.healingTargetProfile(peerSession.deployedCreatureIndex))
	if err != nil {
		return nil, fmt.Errorf("webHealReduction: %w", err)
	}
	maximumHitPoint, maximumManaPoint, err := peerSession.deployedResourceMaximum()
	if err != nil {
		return nil, fmt.Errorf("webHealMaximum: %w", err)
	}
	if maximumManaPoint < 0 {
		return nil, errors.New("web maximum power invalid")
	}
	previousHitPoint := peerSession.deployedHitPoint()
	hitPoint := min(maximumHitPoint, previousHitPoint+healing)
	healedAmount := hitPoint - previousHitPoint
	if healedAmount <= 0 {
		return nil, nil
	}
	isGameOver, err := peerSession.setDeployedHitPoints(hitPoint)
	if err != nil {
		return nil, fmt.Errorf("webHealCommit: %w", err)
	}
	if isGameOver {
		return nil, errors.New("web healing recipient defeated")
	}
	packets, err := abilityraknet.ChannelDrainHealing(e.sourceObjectID, hitPoint, healedAmount)
	if err != nil {
		return nil, fmt.Errorf("webHealEncode: %w", err)
	}
	resourcePacket, err := peerSession.marshalCampaignCharacterResource(peerSession.deployedCreatureIndex)
	if err != nil {
		return nil, fmt.Errorf("webHealResource: %w", err)
	}
	return append(packets, resourcePacket), nil
}

func (e heroDrainSchedule) failWebTransfusionLocked() {
	elapsed := max(time.Duration(0), e.runtime.now().Sub(e.run.areaDrain.startedAt))
	packet, err := abilityraknet.Animation(e.sourceObjectID, e.definition.OutAnimationName,
		e.packet.SourceTime+uint64(elapsed/time.Millisecond))
	if err != nil {
		e.runtime.logger.Printf("RakNet Web Transfusion failure animation failed: %v", err)
		return
	}
	e.run.queueReleaseLocked(e.sessionKey, e.generation, [][]byte{e.run.releasePacket, packet})
}

func (e campaignAbilityCommandRuntime) handleWebTransfusion(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	activeAbilityID uint32, sessionKey string,
	abilityStartTime time.Time,
) ([][]byte, error) {
	if activeAbilityID == 0 || definition.Radius <= 0 || definition.NumberOfTicks == 0 ||
		definition.TickDuration <= 0 || definition.MinimumDamage <= 0 ||
		definition.MaximumDamage < definition.MinimumDamage || definition.LifeSteal <= 0 ||
		definition.AnimationName == "" || definition.OutAnimationName == "" ||
		definition.MuzzleEffectName == "" || definition.HitEffectName == "" ||
		definition.ActivationEffectName == "" {
		e.registry.mutex.Unlock()
		return req.reject("web transfusion definition unavailable")
	}
	if peerSession.heroDrain != nil {
		e.registry.mutex.Unlock()
		return req.reject("channel drain already active")
	}
	projected, err := zoneability.ProjectChannelDrainTiming(creature, definition)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroDrainTiming: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(
		projected.ManaCost, creature.DamageProfile.PrimaryAttribute,
		projected.ManaCoefficient, peerSession.isOverdriveActiveAt(abilityStartTime),
	)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroDrainMana: %w", err)
	}
	if peerSession.deployedManaPoint() < manaCost {
		e.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	channelStart := req.packet.SourceTime + uint64(projected.HitDelay/time.Millisecond)
	channelEnd := channelStart + uint64(projected.Duration/time.Millisecond)
	// Build 103's response handler (0x4D64D0) copies wire offsets 16 and 24
	// into the channel HUD's start/end timestamps. Sending the first-hit time
	// at offset 24 expires both the bar and native key-release handling at 0.3s.
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: req.command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: activeAbilityID, AbilityIndex: req.command.Ability.Index,
		SourceStartMilliseconds:  channelStart,
		SourceCommitMilliseconds: channelEnd,
		SourceEndMilliseconds: channelStart +
			uint64(projected.Cooldown/time.Millisecond),
	})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroDrainAck: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(
		req.command.Common.Unknown[0], activeAbilityID,
		req.command.Ability.Index, req.packet.SourceTime,
		projected.HitDelay, projected.ReleaseDelay,
	)
	if err != nil {
		e.registry.mutex.Unlock()
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
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroDrainStart[%d]: %w", index, marshalErr)
		}
		startPackets = append(startPackets, packet)
	}
	cooldownReservation, isCooldownReserved :=
		peerSession.abilityCooldownSession().Reserve(
			zoneability.HeroAbilityCooldown(activeAbilityID),
			abilityStartTime, projected.HitDelay+projected.Cooldown,
		)
	if !isCooldownReserved {
		e.registry.mutex.Unlock()
		return req.reject("channel drain cooldown unavailable")
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			abilityStartTime, projected.ReleaseDelay,
		)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		e.registry.mutex.Unlock()
		return req.reject("channel drain release unavailable")
	}
	sourceEffectSlot, isSourceEffectAllocated := e.effectPool.Allocate(
		req.command.Common.ObjectID,
	)
	if !isSourceEffectAllocated {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		e.registry.mutex.Unlock()
		return req.reject("channel drain effect unavailable")
	}
	cast := e.registry.reserveChannelCastLocked(peerSession, req.command.Common.ObjectID, cooldownReservation, releaseReservation)
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil {
		err = cast.debitLocked(&peerSession, manaCost)
	}
	if err != nil {
		e.effectPool.Release(req.command.Common.ObjectID, sourceEffectSlot)
		refundPacket, refundErr := cast.failLocked(&peerSession, true, cast.revision)
		if len(refundPacket) != 0 {
			if peerSession.binding.Mode == game.ModeArena {
				peerSession.queuePackets([][]byte{refundPacket})
			} else {
				e.registry.queueChannelPresentationLocked(peerSession.zone, [][]byte{refundPacket})
			}
		}
		e.registry.sessions[sessionKey] = peerSession
		if refundErr != nil {
			e.logger.Printf("RakNet channel drain admission refund failed: %v", refundErr)
		}
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroDrainCommit: %w", err)
	}
	run := &heroDrainRun{
		cast:     cast,
		registry: e.registry, zone: peerSession.zone, timer: e.npc.timer,
		effectPool: e.effectPool, sourceObjectID: req.command.Common.ObjectID,
		sourceEffectSlot: sourceEffectSlot, releasePacket: releasePacket,
		areaDrain: &webTransfusionRun{effects: make(map[uint32]webTransfusionEffect), relaxAnimationName: projected.OutAnimationName, startedAt: abilityStartTime},
	}
	if peerSession.binding.Mode == game.ModeArena {
		run.arenaGameID = peerSession.binding.GameID
	}
	peerSession.heroDrain = run
	generation := peerSession.generation
	binding := peerSession.binding
	e.registry.sessions[sessionKey] = peerSession
	e.registry.mutex.Unlock()

	schedule := heroDrainSchedule{
		runtime: e, packet: req.packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: req.command.Common.ObjectID,
		creature:   creature,
		definition: projected, binding: binding, run: run,
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, projected.NumberOfTicks+1)
	for tick := uint32(0); tick < projected.NumberOfTicks; tick++ {
		deadline := projected.HitDelay + time.Duration(tick)*projected.TickDuration
		step := webTransfusionTickStep{schedule: schedule, deadline: deadline}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: deadline, Produce: step.produce,
		})
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: projected.HitDelay + projected.Duration, Produce: schedule.finish,
	})
	producers = e.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if req.packet.ScheduleGroupResult != nil {
		cancel, err = req.packet.ScheduleGroupResult(producers, schedule.fail)
	} else {
		cancel, err = req.packet.ScheduleGroup(producers)
	}
	if err == nil && cancel == nil {
		err = errors.New("channel cancellation unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		schedule.fail(err)
		return nil, fmt.Errorf("heroDrainSchedule: %w", err)
	}
	run.setCancel(cancel)
	admission := channelDrainAdmission{run: run, revision: cast.revision}
	err = req.packet.AfterResponseCommit(admission.commit)
	if err != nil {
		schedule.fail(err)
		return nil, fmt.Errorf("drainAdmissionCommit: %w", err)
	}
	e.logger.Printf(
		"RakNet hero channel drain accepted ability=%s source=%d ticks=%d channel=%s",
		projected.Name, req.command.Common.ObjectID,
		projected.NumberOfTicks, projected.Duration,
	)
	return append([][]byte{ackPacket}, startPackets...), nil
}
