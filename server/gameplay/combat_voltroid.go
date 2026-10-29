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

type campaignVoltroidActiveBeam struct {
	targetID uint32
	effects  *channelEffects
}

type voltroidDischargeBeamCleanup struct {
	registry *gameplaySessionRegistry
	objectID uint32
	effects  *channelEffects
}

func (e voltroidDischargeBeamCleanup) retire() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	e.effects.Retire()
	for sessionKey, member := range e.registry.sessions {
		beam, isFound := member.campaignNPCVoltroidBeams[e.objectID]
		if isFound && beam.effects == e.effects {
			delete(member.campaignNPCVoltroidBeams, e.objectID)
			e.registry.sessions[sessionKey] = member
		}
	}
	e.registry.commitChannelRemovalsLocked()
}

// Build 103's instant-cast template adds the channel at its hit and removes
// that exact attachment on Deactivate after timetorelease. A chained event
// has no removal identity and leaves the authored beam looping indefinitely.
func (e campaignNPCActionRuntime) startVoltroidDischargeBeamLocked(
	member *gameplayPeerSession, sessionKey string, plan zonenpc.AttackPlan,
) error {
	if e.timer == nil {
		return errors.New("Voltroid discharge timer unavailable")
	}
	member.retireVoltroidBeamsForObject(plan.SourceObjectID)
	e.registry.sessions[sessionKey] = *member
	e.registry.commitChannelRemovalsLocked()
	*member = e.registry.sessions[sessionKey]
	effects, err := e.registry.allocateChannelEffectsLocked(member.zone, e.effectPool, []uint32{plan.SourceObjectID})
	if err != nil {
		return fmt.Errorf("dischargeSlot: %w", err)
	}
	slots := effects.Slots()
	packet, err := npcraknet.VoltroidEffect(plan.SourceObjectID, plan.TargetObjectID,
		slots[0].slot, plan.Profile.TrailEffectName, false)
	if err != nil {
		effects.Retire()
		e.registry.commitChannelRemovalsLocked()
		return fmt.Errorf("dischargeBeam: %w", err)
	}
	cleanup := voltroidDischargeBeamCleanup{registry: e.registry, objectID: plan.SourceObjectID, effects: effects}
	cancel, err := e.timer.Schedule(max(time.Duration(0), plan.Profile.ReleaseDelay-plan.Profile.HitDelay), cleanup.retire)
	if err != nil || cancel == nil {
		if cancel != nil {
			cancel()
		}
		effects.Retire()
		e.registry.commitChannelRemovalsLocked()
		if err != nil {
			return fmt.Errorf("dischargeExpiry: %w", err)
		}
		return errors.New("Voltroid discharge cancellation unavailable")
	}
	if member.campaignNPCVoltroidBeams == nil {
		member.campaignNPCVoltroidBeams = make(map[uint32]campaignVoltroidActiveBeam)
	}
	member.campaignNPCVoltroidBeams[plan.SourceObjectID] = campaignVoltroidActiveBeam{targetID: plan.TargetObjectID, effects: effects}
	e.registry.sessions[sessionKey] = *member
	err = e.registry.publishChannelEffectsLocked(effects, [][]byte{packet})
	*member = e.registry.sessions[sessionKey]
	if err != nil {
		cancel()
		member.retireVoltroidBeamsForObject(plan.SourceObjectID)
		e.registry.sessions[sessionKey] = *member
		e.registry.commitChannelRemovalsLocked()
		return fmt.Errorf("dischargePublish: %w", err)
	}
	return nil
}

func (e *gameplayPeerSession) retireVoltroidBeamsForObject(objectID uint32) {
	for sourceID, beam := range e.campaignNPCVoltroidBeams {
		if sourceID != objectID && beam.targetID != objectID {
			continue
		}
		beam.effects.Retire()
		delete(e.campaignNPCVoltroidBeams, sourceID)
	}
}

// A death can interrupt the transfer before its normal beam retirement.
func (r campaignNPCActionRuntime) stopVoltroidBeamsForObject(
	sessionKey string, generation uint64, objectID uint32,
) {
	r.registry.mutex.Lock()
	member, isFound := r.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil {
		r.registry.mutex.Unlock()
		return
	}
	for candidateKey, candidate := range r.registry.sessions {
		if candidate.zone != member.zone {
			continue
		}
		candidate.retireVoltroidBeamsForObject(objectID)
		r.registry.sessions[candidateKey] = candidate
	}
	for _, retained := range r.registry.retainedSessions {
		if retained.peerSession.zone == member.zone {
			retained.peerSession.retireVoltroidBeamsForObject(objectID)
		}
	}
	r.registry.commitChannelRemovalsLocked()
	r.registry.mutex.Unlock()
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
	charge               *campaignVoltroidPendingCharge
	castTimestamp        uint64
	castAt               time.Time
}

func (e campaignVoltroidSchedule) fail(
	step string, err error,
) ([][]byte, error) {
	e.releaseCharge()
	e.runtime.releaseActionGeneration(e.sessionKey, e.generation, e.objectID, e.plan.ActionGeneration)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e campaignVoltroidSchedule) next() ([][]byte, error) {
	e.releaseCharge()
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceGenerationActive(e.generation, e.objectID, e.plan.ActionGeneration)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
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

func (e campaignVoltroidSchedule) transfer() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.isCampaignNPCSourceGenerationActive(e.generation, e.objectID, e.plan.ActionGeneration)
	if !isCurrent {
		e.releaseChargeLocked()
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	e.stopBeamLocked()
	var beamPackets [][]byte
	// Beam retirement queues recipient batches, including the source. Reload
	// its aggregate before updating earned counters so that FIFO is preserved.
	peerSession = e.runtime.registry.sessions[e.sessionKey]
	if e.charge == nil || !e.charge.isHitAccepted ||
		!e.charge.npc.CompleteZapCharge(e.charge.lease) {
		e.releaseChargeLocked()
		e.runtime.registry.mutex.Unlock()
		return beamPackets, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.targetID)
	if !isSourceFound || !isTargetFound || target.IsDefeated ||
		target.Faction != source.Faction || zonegeometry.Distance(
		source.Plan.Position, target.Plan.Position,
	) > 16 {
		e.runtime.registry.mutex.Unlock()
		return beamPackets, nil
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
		return beamPackets, nil
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
		cleanupPackets, cleanupErr := cleanup.produce()
		if cleanupErr != nil {
			return beamPackets, fmt.Errorf("voltroidChargeCleanup: %w", cleanupErr)
		}
		return append(beamPackets, cleanupPackets...), nil
	}
	if cancelCleanup == nil {
		e.runtime.logger.Printf(
			"RakNet Voltroid charge cleanup cannot be cancelled object=%d target=%d",
			e.objectID, e.targetID,
		)
	}
	packets := append([][]byte(nil), beamPackets...)
	if isEffectReplacement {
		removePacket, removeErr := npcraknet.VoltroidEffect(
			e.targetID, 0, effectSlot, "", true,
		)
		if removeErr != nil {
			return packets, fmt.Errorf("voltroidChargeReplace: %w", removeErr)
		}
		packets = append(packets, removePacket)
	}
	packet, err := npcraknet.VoltroidEffect(
		e.targetID, 0, effectSlot, effectName, false,
	)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet Voltroid charge presentation omitted object=%d target=%d: %v",
			e.objectID, e.targetID, err,
		)
		return packets, nil
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
		plan: zonenpc.AttackPlan{ActionGeneration: source.ActionGeneration},
	}
	profile, isProfileFound := zonenpc.CitadelSpecificOneChargeFriendProfile(
		source.Plan.NounName,
	)
	if !isProfileFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, nil
	}
	profile = zonenpc.ApplyAffixTiming(profile, source.Plan)
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
	startPackets, startErr := schedule.start(timestamp)
	if startErr != nil {
		return nil, true, fmt.Errorf("voltroidChargeStart: %w", startErr)
	}
	return startPackets, true, nil
}
