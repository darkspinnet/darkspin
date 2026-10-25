package gameplay

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignNPCPullEffectRun struct {
	mutex          sync.Mutex
	pool           *attachedEffectPool
	targetObjectID uint32
	attachments    []campaignNPCPullAttachment
	isReleased     bool
}

type campaignNPCPullAttachment struct {
	objectID uint32
	slot     uint8
}

func (e *campaignNPCPullEffectRun) release() ([]campaignNPCPullAttachment, bool) {
	if e == nil {
		return nil, false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isReleased {
		return nil, false
	}
	e.isReleased = true
	for _, attachment := range e.attachments {
		if e.pool == nil {
			continue
		}
		isReleased := e.pool.Release(attachment.objectID, attachment.slot)
		if !isReleased {
			// A concurrent actor cleanup may already have cleared this slot.
		}
	}
	attachments := e.attachments
	e.attachments = nil
	return attachments, true
}

func campaignNPCPullRemovalPackets(attachments []campaignNPCPullAttachment) ([][]byte, error) {
	packets := make([][]byte, 0, len(attachments))
	for index, attachment := range attachments {
		packet, err := npcraknet.ForcedMovementEffect(attachment.objectID, attachment.slot, "", true)
		if err != nil {
			return nil, fmt.Errorf("pullEffectRemove[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e *campaignNPCPullEffectRun) targets(objectID uint32) bool {
	if e == nil || objectID == 0 {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return !e.isReleased && e.targetObjectID == objectID
}

func (s *gameplayPeerSession) stopCampaignNPCPullEffectsTargeting(
	targetObjectID uint32,
) ([][]byte, error) {
	if s == nil || targetObjectID == 0 {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for sourceObjectID, run := range s.campaignNPCPullEffects {
		if !run.targets(targetObjectID) {
			continue
		}
		delete(s.campaignNPCPullEffects, sourceObjectID)
		attachments, isReleased := run.release()
		if !isReleased {
			continue
		}
		removalPackets, err := campaignNPCPullRemovalPackets(attachments)
		if err != nil {
			return nil, fmt.Errorf("pullerTargetEffect: %w", err)
		}
		packets = append(packets, removalPackets...)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) produceGrapplingPulsarPull(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	enemy, isEnemyFound := zonenpc.Snapshot{}, false
	if isCurrent {
		enemy, isEnemyFound = peerSession.zone.NPCs().NPC(objectID)
	}
	profile, isProfileFound := zonenpc.VerdanthSpecialThreePullProfile(
		enemy.Plan.NounName,
	)
	if peerSession.campaignNPCPullReadiness == nil {
		peerSession.campaignNPCPullReadiness = make(map[uint32]uint64)
	}
	readyTimestamp := peerSession.campaignNPCPullReadiness[objectID]
	if !isCurrent || !isEnemyFound || !isProfileFound || readyTimestamp > timestamp {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	profile = zonenpc.ApplyAffixTiming(profile, enemy.Plan)
	target, isTargetFound := peerSession.campaignNPCTarget(generation, enemy.TargetObjectID)
	swipeProfile, isSwipeFound := zonenpc.VerdanthSpecialThreeFastSwipeProfile(enemy.Plan.NounName)
	if !isTargetFound || !isSwipeFound || zonegeometry.Distance(enemy.Plan.Position, target.Position)-
		enemy.Plan.ActorFootprintRadius()-target.FootprintRadius <= swipeProfile.Range {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	_, slowAttackScale := peerSession.zone.NPCs().SlowProfile(objectID, r.now())
	cooldownProfile := applyNPCSlowTiming(profile, slowAttackScale)
	reservedTimestamp := timestamp + uint64(cooldownProfile.Cooldown/time.Millisecond)
	peerSession.campaignNPCPullReadiness[objectID] = reservedTimestamp
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	packets, err := r.produceZelemShotWithProfile(
		packet, sessionKey, generation, objectID, timestamp, profile,
	)
	if err != nil {
		r.rollbackGrapplingPulsarPull(
			sessionKey, generation, objectID, readyTimestamp, reservedTimestamp,
		)
		return nil, false, fmt.Errorf("enemyPullerProjectile: %w", err)
	}
	return packets, true, nil
}

func (r campaignNPCActionRuntime) rollbackGrapplingPulsarPull(
	sessionKey string, generation uint64, objectID uint32,
	readyTimestamp uint64, reservedTimestamp uint64,
) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.campaignNPCPullReadiness[objectID] == reservedTimestamp
	if isCurrent {
		peerSession.campaignNPCPullReadiness[objectID] = readyTimestamp
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
}

func (r campaignNPCActionRuntime) startGrapplingPulsarPullEffect(
	packet raknet.Packet, peerSession *gameplayPeerSession,
	sessionKey string, generation uint64, sourceObjectID uint32,
	objectID uint32, effectName string, duration time.Duration,
) ([][]byte, error) {
	if r.effectPool == nil || peerSession == nil || sessionKey == "" ||
		generation == 0 || sourceObjectID == 0 || objectID == 0 ||
		effectName == "" || duration <= 0 {
		return nil, nil
	}
	run := &campaignNPCPullEffectRun{
		pool: r.effectPool, targetObjectID: objectID,
	}
	packets := make([][]byte, 0, 2)
	// Authored PullModifier owns its hit orb on the victim and its beam on
	// the initiator, linked to the victim. Both are removed after pullDuration.
	if effectName == "puller_beam_attack_beam.ServerEventDef" {
		hitPacket, err := run.attach(objectID, 0, "puller_beam_attack_hit.ServerEventDef")
		if err != nil {
			return nil, fmt.Errorf("pullHitStart: %w", err)
		}
		packets = append(packets, hitPacket)
		beamPacket, err := run.attach(sourceObjectID, objectID, effectName)
		if err != nil {
			_, isReleased := run.release()
			if !isReleased {
				// No presentation was published; another cleanup owns the slots.
			}
			return nil, fmt.Errorf("pullBeamStart: %w", err)
		}
		packets = append(packets, beamPacket)
	} else {
		effectPacket, err := run.attach(objectID, 0, effectName)
		if err != nil {
			return nil, fmt.Errorf("pullEffectStart: %w", err)
		}
		packets = append(packets, effectPacket)
	}
	if peerSession.campaignNPCPullEffects == nil {
		peerSession.campaignNPCPullEffects = make(map[uint32]*campaignNPCPullEffectRun)
	}
	peerSession.campaignNPCPullEffects[sourceObjectID] = run
	cleanup := campaignNPCPullEffectSchedule{
		runtime: r, sessionKey: sessionKey, generation: generation,
		sourceObjectID: sourceObjectID, run: run,
	}
	cancel, err := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{{
		Delay: duration, Produce: cleanup.remove,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		delete(peerSession.campaignNPCPullEffects, sourceObjectID)
		_, isReleased := run.release()
		if !isReleased {
			// The scheduler failed before another cleanup path could own the run.
		}
		return nil, fmt.Errorf("pullerEffectSchedule: %w", err)
	}
	return packets, nil
}

func (e *campaignNPCPullEffectRun) attach(objectID, secondaryObjectID uint32, effectName string) ([]byte, error) {
	slot, isAllocated := e.pool.Allocate(objectID)
	if !isAllocated {
		return nil, errors.New("pull attachment slots exhausted")
	}
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		ObjectID: objectID, SecondaryObjectID: secondaryObjectID,
		Slot: slot + 1, Asset: util.HashID(effectName), IsForceAttached: true,
	})
	if err != nil {
		isReleased := e.pool.Release(objectID, slot)
		if !isReleased {
			// The attachment was never published and owns no remaining slot.
		}
		return nil, fmt.Errorf("pullEffectEncode: %w", err)
	}
	e.attachments = append(e.attachments, campaignNPCPullAttachment{objectID: objectID, slot: slot})
	return packet, nil
}

func (r campaignNPCActionRuntime) stopGrapplingPulsarPullEffects(
	sessionKey string, generation uint64, sourceObjectID uint32,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	runs := make([]*campaignNPCPullEffectRun, 0)
	for candidateKey, candidate := range r.registry.sessions {
		if candidate.zone != peerSession.zone {
			continue
		}
		run := candidate.campaignNPCPullEffects[sourceObjectID]
		if run == nil {
			continue
		}
		delete(candidate.campaignNPCPullEffects, sourceObjectID)
		r.registry.sessions[candidateKey] = candidate
		runs = append(runs, run)
	}
	r.registry.mutex.Unlock()
	packets := make([][]byte, 0, len(runs))
	for index, run := range runs {
		attachments, isReleased := run.release()
		if !isReleased {
			continue
		}
		removalPackets, err := campaignNPCPullRemovalPackets(attachments)
		if err != nil {
			return nil, fmt.Errorf("pullerDefeatEffect[%d]: %w", index, err)
		}
		packets = append(packets, removalPackets...)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) applyGrapplingPulsarPull(
	packet raknet.Packet, sessionKey string, generation uint64,
	plan zonenpc.AttackPlan, target zone.NPCTarget, timestamp uint64,
) ([][]byte, error) {
	modifierPackets, err := r.applyCampaignNPCTimedModifier(
		packet, sessionKey, generation, plan, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("pullerModifier: %w", err)
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		generation, plan.SourceObjectID,
	)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return modifierPackets, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(plan.SourceObjectID)
	liveTarget, isTargetFound := peerSession.campaignNPCTarget(
		generation, plan.TargetObjectID,
	)
	if !isEnemyFound || !isTargetFound || liveTarget.ObjectID != target.ObjectID {
		r.registry.mutex.Unlock()
		return modifierPackets, nil
	}
	if liveTarget.IsHero && peerSession.isEnemyRootActive(r.now()) {
		r.registry.mutex.Unlock()
		return modifierPackets, nil
	}
	plan.SourcePosition = enemy.Plan.Position
	plan.TargetPosition = liveTarget.Position
	distance := zonegeometry.Distance(plan.SourcePosition, plan.TargetPosition)
	switch {
	case distance > 10:
		plan.Profile.ForcedMovementReactionName = "react_pulled_far"
	case distance > 5:
		plan.Profile.ForcedMovementReactionName = "react_pulled_med"
	default:
		plan.Profile.ForcedMovementReactionName = "react_pulled_short"
	}
	movementPlan := plan
	movementPlan.Profile.ForcedMovementEffectName = ""
	movementPackets, err := r.applyEnemyForcedMovement(
		&peerSession, movementPlan, liveTarget, timestamp,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("pullerMovement: %w", err)
	}
	if len(movementPackets) != 0 {
		effectPackets, effectErr := r.startGrapplingPulsarPullEffect(
			packet, &peerSession, sessionKey, generation,
			plan.SourceObjectID, liveTarget.ObjectID,
			plan.Profile.ForcedMovementEffectName, plan.Profile.ForcedMovementDuration,
		)
		if effectErr != nil {
			r.logger.Printf(
				"RakNet Grappling Pulsar pull effect omitted source=%d target=%d: %v",
				plan.SourceObjectID, liveTarget.ObjectID, effectErr,
			)
		} else {
			movementPackets = append(movementPackets, effectPackets...)
		}
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	return append(modifierPackets, movementPackets...), nil
}
