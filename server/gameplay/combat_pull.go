package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

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
	packet raknet.Packet, objectID uint32, effectName string,
	duration time.Duration,
) ([]byte, error) {
	if r.effectPool == nil || objectID == 0 || effectName == "" || duration <= 0 {
		return nil, nil
	}
	effectSlot, isAllocated := r.effectPool.Allocate(objectID)
	if !isAllocated {
		return nil, nil
	}
	effectPacket, err := npcraknet.ForcedMovementEffect(
		objectID, effectSlot, effectName, false,
	)
	if err != nil {
		r.effectPool.Release(objectID, effectSlot)
		return nil, fmt.Errorf("pullerEffectStart: %w", err)
	}
	cleanup := campaignNPCPullEffectSchedule{
		runtime: r, objectID: objectID, slot: effectSlot,
	}
	cancel, err := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{{
		Delay: duration, Produce: cleanup.remove,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		r.effectPool.Release(objectID, effectSlot)
		return nil, fmt.Errorf("pullerEffectSchedule: %w", err)
	}
	return effectPacket, nil
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
		effectPacket, effectErr := r.startGrapplingPulsarPullEffect(
			packet, liveTarget.ObjectID, plan.Profile.ForcedMovementEffectName,
			plan.Profile.ForcedMovementDuration,
		)
		if effectErr != nil {
			r.logger.Printf(
				"RakNet Grappling Pulsar pull effect omitted source=%d target=%d: %v",
				plan.SourceObjectID, liveTarget.ObjectID, effectErr,
			)
		} else if effectPacket != nil {
			movementPackets = append(movementPackets, effectPacket)
		}
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	return append(modifierPackets, movementPackets...), nil
}
