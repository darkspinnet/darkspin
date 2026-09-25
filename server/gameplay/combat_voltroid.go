package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const (
	campaignVoltroidDischargeStackCount  = 5
	campaignVoltroidChargeFriendCooldown = 15 * time.Second
)

type campaignVoltroidVisualCleanupStep struct {
	runtime    campaignNPCActionRuntime
	objectID   uint32
	effectSlot uint8
}

func (e campaignVoltroidVisualCleanupStep) produce() ([][]byte, error) {
	if !e.runtime.effectPool.Release(e.objectID, e.effectSlot) {
		return nil, nil
	}
	packet, err := npcraknet.VoltroidEffect(
		e.objectID, 0, e.effectSlot, "", true,
	)
	if err != nil {
		return nil, fmt.Errorf("voltroidVisualRemove: %w", err)
	}
	return [][]byte{packet}, nil
}

type campaignVoltroidChargeCleanupStep struct {
	runtime    campaignNPCActionRuntime
	sessionKey string
	generation uint64
	objectID   uint32
	effectSlot uint8
	expiresAt  uint64
}

func (e campaignVoltroidChargeCleanupStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.campaignNPCVoltroidChargeExpires[e.objectID] == e.expiresAt
	effectSlot, isEffectFound :=
		peerSession.campaignNPCVoltroidEffectSlots[e.objectID]
	if !isCurrent || !isEffectFound || effectSlot != e.effectSlot {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	delete(peerSession.campaignNPCVoltroidCharges, e.objectID)
	delete(peerSession.campaignNPCVoltroidChargeExpires, e.objectID)
	delete(peerSession.campaignNPCVoltroidEffectSlots, e.objectID)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	if !e.runtime.effectPool.Release(e.objectID, e.effectSlot) {
		return nil, nil
	}
	packet, err := npcraknet.VoltroidEffect(
		e.objectID, 0, e.effectSlot, "", true,
	)
	if err != nil {
		return nil, fmt.Errorf("voltroidChargeRemove: %w", err)
	}
	return [][]byte{packet}, nil
}

type campaignVoltroidSchedule struct {
	runtime              campaignNPCActionRuntime
	packet               raknet.Packet
	sessionKey           string
	generation           uint64
	objectID             uint32
	targetID             uint32
	timestamp            uint64
	chargeReadyTimestamp uint64
	plan                 zonenpc.AttackPlan
}

func (e campaignVoltroidSchedule) fail(
	step string, err error,
) ([][]byte, error) {
	e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e campaignVoltroidSchedule) next() ([][]byte, error) {
	packets, err := e.runtime.produceEnemyMelee(
		e.packet, e.sessionKey, e.generation, e.objectID, e.timestamp,
	)
	if err != nil {
		return e.fail("voltroidNext", err)
	}
	return packets, nil
}

func (e campaignVoltroidSchedule) arrive(timestamp uint64) ([][]byte, error) {
	packets, err := e.runtime.produceEnemyMelee(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
	if err != nil {
		return e.fail("voltroidArrive", err)
	}
	return packets, nil
}

func (e campaignVoltroidSchedule) hit() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.isCampaignNPCSourceActive(e.generation, e.objectID)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.targetID)
	if !isSourceFound || !isTargetFound || target.IsDefeated ||
		target.Faction != source.Faction || zonegeometry.Distance(
		source.Plan.Position, target.Plan.Position,
	) > 16 {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.campaignNPCVoltroidCharges == nil {
		peerSession.campaignNPCVoltroidCharges = make(map[uint32]uint32)
	}
	if peerSession.campaignNPCVoltroidChargeExpires == nil {
		peerSession.campaignNPCVoltroidChargeExpires = make(map[uint32]uint64)
	}
	if peerSession.campaignNPCVoltroidChargeReadiness == nil {
		peerSession.campaignNPCVoltroidChargeReadiness = make(map[uint32]uint64)
	}
	if peerSession.campaignNPCVoltroidEffectSlots == nil {
		peerSession.campaignNPCVoltroidEffectSlots = make(map[uint32]uint8)
	}
	stackCount := peerSession.campaignNPCVoltroidCharges[e.targetID]
	if stackCount < campaignVoltroidDischargeStackCount {
		stackCount++
		peerSession.campaignNPCVoltroidCharges[e.targetID] = stackCount
	}
	expiresAt := e.chargeReadyTimestamp
	peerSession.campaignNPCVoltroidChargeExpires[e.targetID] = expiresAt
	peerSession.campaignNPCVoltroidChargeReadiness[e.objectID] =
		e.chargeReadyTimestamp
	effectSlot, isEffectFound :=
		peerSession.campaignNPCVoltroidEffectSlots[e.targetID]
	isEffectReplacement := isEffectFound
	if !isEffectFound {
		var isAllocated bool
		effectSlot, isAllocated = e.runtime.effectPool.Allocate(e.targetID)
		if isAllocated {
			peerSession.campaignNPCVoltroidEffectSlots[e.targetID] = effectSlot
			isEffectFound = true
		}
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	if !isEffectFound {
		e.runtime.logger.Printf(
			"RakNet Voltroid charge presentation omitted object=%d target=%d: effect slots unavailable",
			e.objectID, e.targetID,
		)
		return nil, nil
	}
	effectName := fmt.Sprintf(
		"citadel_zap_has_charge_%d.ServerEventDef", min(stackCount, uint32(4)),
	)
	cleanup := campaignVoltroidChargeCleanupStep{
		runtime: e.runtime, sessionKey: e.sessionKey,
		generation: e.generation, objectID: e.targetID,
		effectSlot: effectSlot, expiresAt: expiresAt,
	}
	cancelCleanup, scheduleErr := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{
		Delay: campaignVoltroidChargeFriendCooldown, Produce: cleanup.produce,
	}})
	if scheduleErr != nil {
		e.runtime.logger.Printf(
			"RakNet Voltroid charge cleanup not scheduled object=%d target=%d: %v",
			e.objectID, e.targetID, scheduleErr,
		)
		return cleanup.produce()
	}
	if cancelCleanup == nil {
		e.runtime.logger.Printf(
			"RakNet Voltroid charge cleanup cannot be cancelled object=%d target=%d",
			e.objectID, e.targetID,
		)
	}
	packets := make([][]byte, 0, 2)
	if isEffectReplacement {
		removePacket, removeErr := npcraknet.VoltroidEffect(
			e.targetID, 0, effectSlot, "", true,
		)
		if removeErr != nil {
			return nil, fmt.Errorf("voltroidChargeReplace: %w", removeErr)
		}
		packets = append(packets, removePacket)
	}
	packet, err := npcraknet.VoltroidEffect(
		e.targetID, e.objectID, effectSlot, effectName, false,
	)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet Voltroid charge presentation omitted object=%d target=%d: %v",
			e.objectID, e.targetID, err,
		)
		return nil, nil
	}
	return append(packets, packet), nil
}

func (r campaignNPCActionRuntime) produceCitadelSpecificOne(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		generation, objectID,
	)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(objectID)
	if !isSourceFound || campaignDifficultyNounFamily(source.Plan.NounName) !=
		"citadelspecificone" {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	if peerSession.campaignNPCVoltroidCharges == nil {
		peerSession.campaignNPCVoltroidCharges = make(map[uint32]uint32)
	}
	if peerSession.campaignNPCVoltroidChargeExpires == nil {
		peerSession.campaignNPCVoltroidChargeExpires = make(map[uint32]uint64)
	}
	if peerSession.campaignNPCVoltroidChargeReadiness == nil {
		peerSession.campaignNPCVoltroidChargeReadiness = make(map[uint32]uint64)
	}
	if timestamp < peerSession.campaignNPCVoltroidChargeReadiness[objectID] {
		r.registry.sessions[sessionKey] = peerSession
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	delete(peerSession.campaignNPCVoltroidChargeReadiness, objectID)
	_, isChargeEffectFound :=
		peerSession.campaignNPCVoltroidEffectSlots[objectID]
	if timestamp >= peerSession.campaignNPCVoltroidChargeExpires[objectID] &&
		!isChargeEffectFound {
		delete(peerSession.campaignNPCVoltroidCharges, objectID)
		delete(peerSession.campaignNPCVoltroidChargeExpires, objectID)
	}
	if peerSession.campaignNPCVoltroidCharges[objectID] >=
		campaignVoltroidDischargeStackCount {
		r.registry.sessions[sessionKey] = peerSession
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	var target zonenpc.Snapshot
	for _, candidate := range peerSession.zone.NPCs().Snapshots() {
		if candidate.Plan.ObjectID == objectID || candidate.IsDefeated ||
			candidate.Faction != source.Faction ||
			campaignDifficultyNounFamily(candidate.Plan.NounName) !=
				"citadelspecificone" ||
			peerSession.campaignNPCVoltroidCharges[candidate.Plan.ObjectID] >=
				campaignVoltroidDischargeStackCount ||
			zonegeometry.Distance(
				source.Plan.Position, candidate.Plan.Position,
			) > 16 {
			continue
		}
		target = candidate
		break
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	if target.Plan.ObjectID == 0 {
		return nil, false, nil
	}
	schedule := campaignVoltroidSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, targetID: target.Plan.ObjectID,
		timestamp: timestamp + uint64(time.Second/time.Millisecond),
		chargeReadyTimestamp: timestamp +
			uint64(campaignVoltroidChargeFriendCooldown/time.Millisecond),
		plan: zonenpc.AttackPlan{},
	}
	profile, isProfileFound := zonenpc.CitadelSpecificOneChargeFriendProfile(
		source.Plan.NounName,
	)
	if !isProfileFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, nil
	}
	repeatDelay := max(profile.Cooldown, profile.ReleaseDelay)
	schedule.timestamp = timestamp + uint64(repeatDelay/time.Millisecond)
	action, err := campaignNPCActionWithProfile(
		source.Plan, target.Plan.ObjectID, target.Plan.Position, profile,
		target.Plan.NPCProfile.FootprintRadius,
	)
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, fmt.Errorf("voltroidChargeAction: %w", err)
	}
	if action.IsPursuitNeeded {
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			return nil, true, fmt.Errorf("voltroidChargePursuit: %w", marshalErr)
		}
		scheduleErr := r.pursuit.scheduleTarget(
			packet, sessionKey, generation, objectID, target.Plan.ObjectID,
			timestamp, action.TargetPosition, profile, schedule.arrive,
		)
		if scheduleErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			return nil, true, fmt.Errorf("voltroidChargePursuitSchedule: %w", scheduleErr)
		}
		return pursuitPackets, true, nil
	}
	schedule.plan = zonenpc.AttackPlan{
		ActionGeneration: source.ActionGeneration,
		SourceObjectID:   objectID, TargetObjectID: target.Plan.ObjectID,
		SourcePosition: source.Plan.Position, TargetPosition: target.Plan.Position,
		Profile: profile,
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, schedule.plan, timestamp)
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, fmt.Errorf("voltroidChargeStart: %w", err)
	}
	effectSlot, isEffectAllocated := r.effectPool.Allocate(objectID)
	if !isEffectAllocated {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, errors.New("voltroid charge effect slots unavailable")
	}
	beamPacket, err := npcraknet.VoltroidEffect(
		objectID, target.Plan.ObjectID, effectSlot,
		profile.TrailEffectName, false,
	)
	if err != nil {
		isEffectReleased := r.effectPool.Release(objectID, effectSlot)
		if !isEffectReleased {
			r.logger.Printf(
				"RakNet Voltroid charge beam slot already released object=%d slot=%d",
				objectID, effectSlot,
			)
		}
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, fmt.Errorf("voltroidChargeBeam: %w", err)
	}
	cleanup := campaignVoltroidVisualCleanupStep{
		runtime: r, objectID: objectID, effectSlot: effectSlot,
	}
	_, err = scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{
		{Delay: profile.HitDelay, Produce: schedule.hit},
		{Delay: profile.ReleaseDelay, Produce: cleanup.produce},
		{Delay: repeatDelay, Produce: schedule.next},
	})
	if err != nil {
		isEffectReleased := r.effectPool.Release(objectID, effectSlot)
		if !isEffectReleased {
			r.logger.Printf(
				"RakNet Voltroid charge cleanup slot already released object=%d slot=%d",
				objectID, effectSlot,
			)
		}
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, fmt.Errorf("voltroidChargeSchedule: %w", err)
	}
	return append(startPackets, beamPacket), true, nil
}
