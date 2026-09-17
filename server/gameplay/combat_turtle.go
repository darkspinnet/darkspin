package gameplay

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const (
	nomadSpecialThreeTurtleStartDelay = 1238095045 * time.Nanosecond
	nomadSpecialThreeTurtleRecovery   = 857142985 * time.Nanosecond
)

type campaignNomadSpecialThreeTurtleRun struct {
	runtime          campaignDamageRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	objectID         uint32
	timestamp        uint64
	profile          zonenpc.ActionProfile
	modifier         *campaignNPCModifierRun
	center           game.Vec3
	passiveDefense   float32
	effectSlot       uint8
	mutex            sync.Mutex
	isEffectAttached bool
	isFinished       bool
}

type campaignNomadSpecialThreeTurtleStep struct {
	run       *campaignNomadSpecialThreeTurtleRun
	tickIndex uint32
}

func (e campaignNomadSpecialThreeTurtleStep) pulse() ([][]byte, error) {
	return e.run.pulse(e.tickIndex)
}

func (e *campaignNomadSpecialThreeTurtleRun) isCurrent() bool {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if isCurrent {
		enemy, isEnemyFound := peerSession.zone.NPCs().NPC(e.objectID)
		isCurrent = isEnemyFound && !enemy.IsDefeated && enemy.IsTurtleActive
	}
	e.runtime.registry.mutex.RUnlock()
	return isCurrent
}

func (e *campaignNomadSpecialThreeTurtleRun) attachEffect() ([][]byte, error) {
	if !e.isCurrent() {
		return nil, nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isEffectAttached {
		return nil, nil
	}
	slot, isAllocated := e.runtime.effectPool.Allocate(e.objectID)
	if !isAllocated {
		return nil, nil
	}
	packet, err := npcraknet.ShieldEffectAsset(
		e.objectID, slot, "nomad_lieu_lf_3_AOE.ServerEventDef", false,
	)
	if err != nil {
		e.runtime.effectPool.Release(e.objectID, slot)
		return nil, fmt.Errorf("turtleEffectAttach: %w", err)
	}
	e.effectSlot = slot
	e.isEffectAttached = true
	return [][]byte{packet}, nil
}

func (e *campaignNomadSpecialThreeTurtleRun) pulse(
	tickIndex uint32,
) ([][]byte, error) {
	if tickIndex >= e.profile.NumberOfTicks {
		return nil, errors.New("turtle tick invalid")
	}
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil &&
		peerSession.zone.NPCRandom() != nil
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
	if !isSourceFound || source.IsDefeated || !source.IsTurtleActive {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packetOut := make([][]byte, 0)
	poisonPlans := make([]zonenpc.AttackPlan, 0)
	statDelta := sporenet.PlayerStatDelta{}
	pulseTimestamp := e.timestamp + uint64(
		(nomadSpecialThreeTurtleStartDelay+
			time.Duration(tickIndex)*time.Second)/time.Millisecond,
	)
	for _, target := range campaignLobAreaTargets(
		peerSession.zone.LiveNPCTargets(), e.center, e.profile.Radius,
	) {
		plan, err := zonenpc.PlanAreaAttackWithProfile(
			source, target.ObjectID, target.Position, e.profile,
		)
		if err != nil {
			continue
		}
		result, err := zonenpc.CommitAttack(
			peerSession.zone.NPCRandom(), plan,
			source.Plan.NPCProfile.CriticalRating, e.runtime.npc.program.Critical,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("turtleCommit: %w", err)
		}
		hitPackets, targetStatDelta, isApplied, err :=
			e.runtime.npc.applyEnemyAreaAttackDamage(
				&peerSession, e.generation, plan, result, pulseTimestamp,
			)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("turtleDamage: %w", err)
		}
		packetOut = append(packetOut, hitPackets...)
		statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
		if isApplied {
			poisonPlans = append(poisonPlans, plan)
		}
		impactPacket, err := npcraknet.AttackImpact(plan)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("turtleImpact: %w", err)
		}
		packetOut = append(packetOut, impactPacket)
	}
	binding := peerSession.binding
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	for _, plan := range poisonPlans {
		poisonPackets, err := e.runtime.npc.applyCampaignNPCPoison(
			e.packet, e.sessionKey, e.generation, plan, pulseTimestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("turtlePoison: %w", err)
		}
		packetOut = append(packetOut, poisonPackets...)
	}
	err := e.runtime.npc.stats.Record(context.Background(), binding, statDelta)
	if err != nil {
		return nil, fmt.Errorf("turtleStats: %w", err)
	}
	return packetOut, nil
}

func (e *campaignNomadSpecialThreeTurtleRun) recover() ([][]byte, error) {
	e.mutex.Lock()
	packetOut := make([][]byte, 0, 2)
	if e.isEffectAttached &&
		e.runtime.effectPool.Release(e.objectID, e.effectSlot) {
		removePacket, err := npcraknet.ShieldEffectAsset(
			e.objectID, e.effectSlot, "nomad_lieu_lf_3_AOE.ServerEventDef", true,
		)
		if err != nil {
			e.mutex.Unlock()
			return nil, fmt.Errorf("turtleEffectRemove: %w", err)
		}
		e.isEffectAttached = false
		packetOut = append(packetOut, removePacket)
	}
	e.mutex.Unlock()
	if !e.isCurrent() {
		return packetOut, nil
	}
	timestamp := e.timestamp + uint64(
		(nomadSpecialThreeTurtleStartDelay+
			time.Duration(e.profile.NumberOfTicks)*time.Second)/time.Millisecond,
	)
	animationPacket, err := npcraknet.ShieldAnimation(
		e.objectID, "nomad_lieu_lf_3_recover", timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("turtleRecover: %w", err)
	}
	return append(packetOut, animationPacket), nil
}

func (e *campaignNomadSpecialThreeTurtleRun) finish() ([][]byte, error) {
	if !e.finishState() {
		return nil, nil
	}
	timestamp := e.timestamp + uint64(e.duration()/time.Millisecond)
	deletePacket, err := effectraknet.ModifierDelete(
		e.objectID, e.modifier.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("turtleModifierDelete: %w", err)
	}
	resetPacket, err := abilityraknet.AnimationReset(e.objectID, timestamp)
	if err != nil {
		return nil, fmt.Errorf("turtleAnimationReset: %w", err)
	}
	passiveEffectPacket, err := raknet.MarshalApplication(
		raknet.AttachedEffectMessage{
			Slot: 16, IsForceAttached: true,
			Asset:    util.HashID("creature_shield_effect.ServerEventDef"),
			ObjectID: e.objectID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("turtlePassiveEffect: %w", err)
	}
	attributePacket, err := raknet.MarshalApplication(
		raknet.AttributeDataUpdateMessage{
			ObjectID: e.objectID,
			Value: map[uint8]float32{
				uint8(game.AttributeEnergyDefense): e.passiveDefense,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("turtlePassiveDefense: %w", err)
	}
	packetOut := [][]byte{
		deletePacket, resetPacket, passiveEffectPacket, attributePacket,
	}
	actionPackets, err := e.runtime.npc.produceZelemShot(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("turtleActionResume: %w", err)
	}
	return append(packetOut, actionPackets...), nil
}

func (e *campaignNomadSpecialThreeTurtleRun) duration() time.Duration {
	return nomadSpecialThreeTurtleStartDelay +
		time.Duration(e.profile.NumberOfTicks)*time.Second +
		nomadSpecialThreeTurtleRecovery
}

func (e *campaignNomadSpecialThreeTurtleRun) finishState() bool {
	e.mutex.Lock()
	if e.isFinished {
		e.mutex.Unlock()
		return false
	}
	e.isFinished = true
	if e.isEffectAttached {
		e.runtime.effectPool.Release(e.objectID, e.effectSlot)
		e.isEffectAttached = false
	}
	e.mutex.Unlock()
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil &&
		peerSession.campaignNPCModifiers[e.modifier.instanceID] == e.modifier
	isAlive := false
	if isCurrent {
		enemy, isEnemyFound := peerSession.zone.NPCs().NPC(e.objectID)
		isAlive = isEnemyFound && !enemy.IsDefeated && enemy.HitPoint > 0
		endErr := peerSession.zone.NPCs().EndTurtle(e.objectID)
		if endErr == nil {
			peerSession.untrackCampaignNPCModifier(e.modifier)
			e.runtime.registry.sessions[e.sessionKey] = peerSession
		} else {
			isCurrent = false
		}
	}
	e.runtime.registry.mutex.Unlock()
	_, _ = e.modifier.release(e.runtime.npc.modifierPool)
	return isCurrent && isAlive
}

func (e *campaignNomadSpecialThreeTurtleRun) cleanup(_ error) {
	e.finishState()
}

func (r campaignDamageRuntime) startNomadSpecialThreeTurtle(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	modifier, err := newCampaignNPCModifierRun(r.npc.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("turtleModifierRun: %w", err)
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	enemy, isEnemyFound := zonenpc.Snapshot{}, false
	if isCurrent {
		enemy, isEnemyFound = peerSession.zone.NPCs().NPC(objectID)
	}
	profile, isProfileFound := zonenpc.NomadSpecialThreeTurtleProfile(
		enemy.Plan.NounName,
	)
	actionProfile, isActionProfileFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	isCurrent = isCurrent && isEnemyFound && !enemy.IsDefeated &&
		enemy.IsTurtleActive && isProfileFound && isActionProfileFound
	if isCurrent {
		err = peerSession.trackCampaignNPCModifier(modifier)
		if err == nil {
			r.registry.sessions[sessionKey] = peerSession
		}
	}
	r.registry.mutex.Unlock()
	if !isCurrent || err != nil {
		_, releaseErr := modifier.release(r.npc.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("turtleModifierTrack: %w", errors.Join(err, releaseErr))
		}
		return nil, nil
	}
	run := &campaignNomadSpecialThreeTurtleRun{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		profile: profile, modifier: modifier, center: enemy.Plan.Position,
		passiveDefense: enemy.Plan.NPCProfile.ResistRating +
			actionProfile.PassiveEnergyDefense,
	}
	producer := make([]raknet.ScheduledPacketProducer, 0, profile.NumberOfTicks+3)
	producer = append(producer, raknet.ScheduledPacketProducer{
		Delay: nomadSpecialThreeTurtleStartDelay, Produce: run.attachEffect,
	})
	for tickIndex := uint32(0); tickIndex < profile.NumberOfTicks; tickIndex++ {
		step := campaignNomadSpecialThreeTurtleStep{run: run, tickIndex: tickIndex}
		producer = append(producer, raknet.ScheduledPacketProducer{
			Delay: nomadSpecialThreeTurtleStartDelay +
				time.Duration(tickIndex)*time.Second,
			Produce: step.pulse,
		})
	}
	producer = append(producer,
		raknet.ScheduledPacketProducer{
			Delay: nomadSpecialThreeTurtleStartDelay +
				time.Duration(profile.NumberOfTicks)*time.Second,
			Produce: run.recover,
		},
		raknet.ScheduledPacketProducer{Delay: run.duration(), Produce: run.finish},
	)
	producer = r.registry.producerGuard.scheduledProducers(sessionKey, producer)
	var cancel raknet.CancelSchedule
	var scheduleErr error
	if packet.ScheduleGroupResult != nil {
		cancel, scheduleErr = packet.ScheduleGroupResult(producer, run.cleanup)
	} else if packet.ScheduleGroup != nil {
		cancel, scheduleErr = packet.ScheduleGroup(producer)
	} else {
		scheduleErr = errors.New("scheduler unavailable")
	}
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		run.cleanup(scheduleErr)
		return nil, fmt.Errorf("turtleSchedule: %w", scheduleErr)
	}
	r.registry.mutex.Lock()
	latest, isLatestFound := r.registry.sessions[sessionKey]
	isLatest := isLatestFound && latest.generation == generation &&
		latest.campaignNPCModifiers[modifier.instanceID] == modifier
	if isLatest {
		modifier.cancel = cancel
		r.registry.sessions[sessionKey] = latest
	}
	r.registry.mutex.Unlock()
	if !isLatest {
		cancel()
		run.cleanup(nil)
		return nil, nil
	}
	modifier.create()
	modifierPacket, err := effectraknet.ModifierCreate(
		effectraknet.ModifierCreateRequest{
			SourceObjectID: objectID, TargetObjectID: objectID,
			ModifierID: util.HashID("NomadSpecialThreeTurtleModifier"),
			InstanceID: modifier.instanceID, Duration: run.duration(),
			Timestamp: timestamp,
		},
	)
	if err != nil {
		cancel()
		run.cleanup(err)
		return nil, fmt.Errorf("turtleModifierCreate: %w", err)
	}
	animationPacket, err := npcraknet.ShieldAnimation(
		objectID, "nomad_lieu_lf_3_attack2", timestamp,
	)
	if err != nil {
		cancel()
		run.cleanup(err)
		return nil, fmt.Errorf("turtleAnimation: %w", err)
	}
	passiveEffectRemove, err := raknet.MarshalApplication(
		raknet.AttachedEffectMessage{
			Slot: 16, IsRemovalRequested: true, ObjectID: objectID,
		},
	)
	if err != nil {
		cancel()
		run.cleanup(err)
		return nil, fmt.Errorf("turtlePassiveRemove: %w", err)
	}
	attributePacket, err := raknet.MarshalApplication(
		raknet.AttributeDataUpdateMessage{
			ObjectID: objectID,
			Value: map[uint8]float32{
				uint8(game.AttributeEnergyDefense): enemy.Plan.NPCProfile.ResistRating,
			},
		},
	)
	if err != nil {
		cancel()
		run.cleanup(err)
		return nil, fmt.Errorf("turtleDefenseRemove: %w", err)
	}
	return [][]byte{
		modifierPacket, animationPacket, passiveEffectRemove, attributePacket,
	}, nil
}
