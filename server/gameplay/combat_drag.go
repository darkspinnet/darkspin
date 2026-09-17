package gameplay

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const (
	nomadDragSlowShieldCooldown = 16 * time.Second
	nomadDragSlowShieldHitDelay = 233333334 * time.Nanosecond
	nomadDragSlowShieldRelease  = time.Second
	nomadDragTauntRelease       = 1800 * time.Millisecond
)

type campaignNomadDragPhaseStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
}

type campaignNomadDragTauntResetStep struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	actionGeneration uint64
	objectID         uint32
	timestamp        uint64
	profile          zonenpc.ActionProfile
	resume           campaignNPCStrafeResume
}

type campaignNomadDragShieldCleanupStep struct {
	runtime    campaignNPCActionRuntime
	sessionKey string
	generation uint64
	objectID   uint32
	effectSlot uint8
}

type campaignNomadDragResumeStep struct {
	runtime          campaignNPCActionRuntime
	sessionKey       string
	generation       uint64
	actionGeneration uint64
	objectID         uint32
	packet           raknet.Packet
	readyTimestamp   uint64
	resume           campaignNPCStrafeResume
}

func (e campaignNomadDragResumeStep) afterStrafe(timestamp uint64) ([][]byte, error) {
	if timestamp >= e.readyTimestamp {
		packets, err := e.resume(timestamp)
		if err != nil {
			return nil, fmt.Errorf("dragResume: %w", err)
		}
		return packets, nil
	}
	cancel, err := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay:   time.Duration(e.readyTimestamp-timestamp) * time.Millisecond,
		Produce: e.produce,
	}})
	if err != nil {
		return nil, fmt.Errorf("dragCooldown: %w", err)
	}
	if cancel == nil {
		return nil, errors.New("drag cooldown cancellation unavailable")
	}
	return nil, nil
}

func (e campaignNomadDragResumeStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceGenerationActive(
		e.generation, e.objectID, e.actionGeneration,
	)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets, err := e.resume(e.readyTimestamp)
	if err != nil {
		return nil, fmt.Errorf("dragReady: %w", err)
	}
	return packets, nil
}

func (e campaignNomadDragShieldCleanupStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		e.generation, e.objectID,
	)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent || !e.runtime.effectPool.Release(e.objectID, e.effectSlot) {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: e.effectSlot + 1, IsRemovalRequested: true,
		IsHardStop: true, ObjectID: e.objectID,
	})
	if err != nil {
		return nil, fmt.Errorf("dragShieldRemove: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e campaignNomadDragTauntResetStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceGenerationActive(
		e.generation, e.objectID, e.actionGeneration,
	)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	resetPacket, err := npcraknet.ResetAnimation(e.objectID, e.timestamp)
	if err != nil {
		return nil, fmt.Errorf("dragTauntReset: %w", err)
	}
	strafePackets, err := e.runtime.produceBoundedStrafeOrIdle(
		e.packet, e.sessionKey, e.generation, e.objectID,
		e.actionGeneration, e.timestamp, e.profile,
		campaignNPCStrafeModeOrdinary, e.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("dragTauntStrafe: %w", err)
	}
	return append([][]byte{resetPacket}, strafePackets...), nil
}

func (e campaignNomadDragPhaseStep) produce() ([][]byte, error) {
	return e.runtime.producePlunge(
		e.packet, e.sessionKey, e.generation, e.objectID, e.timestamp,
	)
}

func nomadDragSlowShieldPresentation(
	nounName string,
) (float32, string, bool) {
	switch strings.ToLower(nounName) {
	case "nomaddrag.noun":
		return 5.25, "spacetime_aoe_slow_globe_effect.ServerEventDef", true
	case "nomaddrag_2.noun":
		return 6.75, "spacetime_aoe_slow_globe_effect_lvl2.ServerEventDef", true
	case "nomaddrag_3.noun":
		return 8, "spacetime_aoe_slow_globe_effect_lvl3.ServerEventDef", true
	default:
		return 0, "", false
	}
}

func (r campaignNPCActionRuntime) produceNomadDragPhase(
	packet raknet.Packet, sessionKey string, generation uint64,
	enemy zonenpc.Snapshot, target zone.NPCTarget, timestamp uint64,
) ([][]byte, bool, error) {
	radius, effectName, isNomadDrag := nomadDragSlowShieldPresentation(
		enemy.Plan.NounName,
	)
	if !isNomadDrag {
		return nil, false, nil
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		generation, enemy.Plan.ObjectID,
	)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, true, nil
	}
	if peerSession.campaignNPCDragSlowShieldReadiness == nil {
		peerSession.campaignNPCDragSlowShieldReadiness = make(map[uint32]uint64)
	}
	isSlowShieldReady := timestamp >=
		peerSession.campaignNPCDragSlowShieldReadiness[enemy.Plan.ObjectID]
	if isSlowShieldReady {
		peerSession.campaignNPCDragSlowShieldReadiness[enemy.Plan.ObjectID] = timestamp +
			uint64(nomadDragSlowShieldCooldown/time.Millisecond)
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	if isSlowShieldReady {
		effectSlot, isEffectAllocated := r.effectPool.Allocate(enemy.Plan.ObjectID)
		if !isEffectAllocated {
			return nil, true, errors.New("Dimensionist Slow Shield effect unavailable")
		}
		packets, err := nomadDragSlowShieldPackets(
			peerSession.zone.NPCs(), enemy, target, effectName, effectSlot, timestamp,
		)
		if err != nil {
			r.releaseNomadDragShieldEffect(enemy.Plan.ObjectID, effectSlot)
			return nil, true, fmt.Errorf("dragSlowShieldMarshal: %w", err)
		}
		err = r.scheduleNomadDragShield(
			packet, sessionKey, generation, enemy.Plan.ObjectID, effectSlot,
			timestamp,
		)
		if err != nil {
			r.releaseNomadDragShieldEffect(enemy.Plan.ObjectID, effectSlot)
			return nil, true, fmt.Errorf("dragSlowShieldSchedule: %w", err)
		}
		r.logger.Printf(
			"RakNet campaign Dimensionist Slow Shield object=%d radius=%.2f hit=%s",
			enemy.Plan.ObjectID, radius, nomadDragSlowShieldHitDelay,
		)
		return packets, true, nil
	}
	return nil, false, nil
}

func (r campaignNPCActionRuntime) produceNomadDragPostMeteorTaunt(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, actionGeneration uint64, timestamp uint64,
	profile zonenpc.ActionProfile, resume campaignNPCStrafeResume,
) ([][]byte, error) {
	animationName := "nomad_lieu_sp_3_taunt_a"
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceGenerationActive(
		generation, objectID, actionGeneration,
	) && peerSession.zone != nil
	if isCurrent && peerSession.zone.NPCRandom() != nil &&
		peerSession.zone.NPCRandom().Float64() >= 0.5 {
		animationName = "nomad_lieu_sp_3_taunt_b"
	}
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	animationPacket, err := npcraknet.AnimationState(
		objectID, animationName, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("dragTauntMarshal: %w", err)
	}
	cooldownResume := campaignNomadDragResumeStep{
		runtime: r, sessionKey: sessionKey, generation: generation,
		actionGeneration: actionGeneration, objectID: objectID,
		packet: packet, resume: resume,
		readyTimestamp: timestamp + uint64(max(time.Duration(0), profile.Cooldown-profile.ReleaseDelay)/time.Millisecond),
	}
	reset := campaignNomadDragTauntResetStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, actionGeneration: actionGeneration,
		objectID:  objectID,
		timestamp: timestamp + uint64(nomadDragTauntRelease/time.Millisecond),
		profile:   profile, resume: cooldownResume.afterStrafe,
	}
	cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: nomadDragTauntRelease, Produce: reset.produce,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return nil, fmt.Errorf("dragTauntSchedule: %w", err)
	}
	return [][]byte{animationPacket}, nil
}

func nomadDragSlowShieldPackets(
	npcSession *zonenpc.Session,
	enemy zonenpc.Snapshot, target zone.NPCTarget, effectName string,
	effectSlot uint8, timestamp uint64,
) ([][]byte, error) {
	if enemy.Plan.ObjectID == 0 || target.ObjectID == 0 || effectName == "" {
		return nil, errors.New("invalid Dimensionist Slow Shield")
	}
	startPackets, err := marshalNPCAttack(npcSession, zonenpc.AttackPlan{
		SourceObjectID: enemy.Plan.ObjectID, TargetObjectID: target.ObjectID,
		ActionGeneration: enemy.ActionGeneration,
		SourcePosition:   enemy.Plan.Position, TargetPosition: target.Position,
		Profile: zonenpc.ActionProfile{AnimationName: "nomad_lieu_sp_3_attack2"},
	}, timestamp)
	if err != nil {
		return nil, fmt.Errorf("slowShieldStart: %w", err)
	}
	effectPacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: effectSlot + 1, IsForceAttached: true,
		Asset: util.HashID(effectName), ObjectID: enemy.Plan.ObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("slowShieldEffect: %w", err)
	}
	return append(startPackets, effectPacket), nil
}

func (r campaignNPCActionRuntime) releaseNomadDragShieldEffect(
	objectID uint32, effectSlot uint8,
) {
	isReleased := r.effectPool.Release(objectID, effectSlot)
	if !isReleased {
		r.logger.Printf(
			"RakNet Dimensionist Slow Shield effect lease was already released object=%d slot=%d",
			objectID, effectSlot,
		)
	}
}

func (r campaignNPCActionRuntime) scheduleNomadDragShield(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, effectSlot uint8, timestamp uint64,
) error {
	phase := campaignNomadDragPhaseStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID,
		timestamp: timestamp + uint64(nomadDragSlowShieldRelease/time.Millisecond),
	}
	cleanup := campaignNomadDragShieldCleanupStep{
		runtime: r, sessionKey: sessionKey, generation: generation,
		objectID: objectID, effectSlot: effectSlot,
	}
	cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{
		{Delay: nomadDragSlowShieldRelease, Produce: phase.produce},
		{Delay: nomadDragSlowShieldCooldown, Produce: cleanup.produce},
	})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	return err
}
