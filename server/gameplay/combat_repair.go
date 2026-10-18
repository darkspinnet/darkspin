package gameplay

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const reparatronCyberCreatureType = uint32(0)
const reparatronAlertDelay = 1166667 * time.Microsecond
const reparatronReviveDelay = 1900 * time.Millisecond

type campaignReparatronSchedule struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	actionGeneration uint64
	sourceObjectID   uint32
	targetObjectID   uint32
	timestamp        uint64
	profile          zonenpc.ActionProfile
	minionThreshold  uint32
	otherThreshold   uint32
}

// Repair's alert is followed by approach and repeated repair animations, not
// repeated combat selection rolls. See client chunks 825, 763 and 266.
func (e campaignReparatronSchedule) approach(timestamp uint64) ([][]byte, error) {
	e.timestamp = timestamp
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || !peerSession.isCampaignNPCSourceGenerationActive(e.generation, e.sourceObjectID, e.actionGeneration) {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.sourceObjectID)
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.targetObjectID)
	e.runtime.registry.mutex.RUnlock()
	if !isSourceFound || !isTargetFound || !target.IsPublished || !target.IsDefeated {
		return e.next()
	}
	profile := e.profile
	profile.Range = (source.Plan.ActorFootprintRadius() + target.Plan.ActorFootprintRadius()) * 0.95
	movementProfile, isMovementFound := zonenpc.ActionProfileForPlan(source.Plan)
	if isMovementFound {
		profile.MovementSpeed = movementProfile.MovementSpeed
		profile.NonCombatMovementSpeed = movementProfile.NonCombatMovementSpeed
	}
	if zonegeometry.Distance(source.Plan.Position, target.Plan.Position) <= profile.Range {
		return e.repair(timestamp)
	}
	plan := zonenpc.FirstActionPlan{
		ObjectID: e.sourceObjectID, TargetObjectID: e.targetObjectID,
		SourcePosition: source.Plan.Position, TargetPosition: target.Plan.Position,
		Profile: profile, IsPursuitNeeded: true,
	}
	packets, err := npcraknet.Pursuit(plan)
	if err != nil {
		return nil, fmt.Errorf("repairPursuit: %w", err)
	}
	err = e.runtime.pursuit.scheduleTarget(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID, e.targetObjectID,
		timestamp, target.Plan.Position, profile, e.approach,
	)
	if err != nil {
		return nil, fmt.Errorf("repairApproach: %w", err)
	}
	return packets, nil
}

func (e campaignReparatronSchedule) begin() ([][]byte, error) {
	return e.approach(e.timestamp)
}

func (e campaignReparatronSchedule) repair(timestamp uint64) ([][]byte, error) {
	isDeferred, err := e.runtime.pursuit.deferAction(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID, timestamp, e.approach,
	)
	if err != nil {
		return nil, fmt.Errorf("repairDefer: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || !peerSession.isCampaignNPCSourceGenerationActive(e.generation, e.sourceObjectID, e.actionGeneration) {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.sourceObjectID)
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.targetObjectID)
	if !isSourceFound || !isTargetFound || !target.IsDefeated || !target.IsPublished {
		e.runtime.registry.mutex.RUnlock()
		return e.next()
	}
	isFacingCommitted := peerSession.zone.NPCs().CommitFacing(zonenpc.AttackPlan{
		SourceObjectID: e.sourceObjectID, TargetObjectID: e.targetObjectID,
		ActionGeneration: e.actionGeneration, SourcePosition: source.Plan.Position, TargetPosition: target.Plan.Position,
	})
	e.runtime.registry.mutex.RUnlock()
	if !isFacingCommitted {
		return nil, nil
	}
	packets, err := npcraknet.RepairCast(source, target, timestamp)
	if err != nil {
		return nil, fmt.Errorf("repairCast: %w", err)
	}
	e.timestamp = timestamp + uint64(e.profile.HitDelay/time.Millisecond)
	err = scheduleNPCProducer(e.runtime.registry, e.packet, e.profile.HitDelay, e.hit)
	if err != nil {
		return nil, fmt.Errorf("repairHitSchedule: %w", err)
	}
	return packets, nil
}

func (e campaignReparatronSchedule) next() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceGenerationActive(e.generation, e.sourceObjectID, e.actionGeneration)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets, err := e.runtime.produceDronePunch(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID, e.timestamp,
	)
	if err != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.sourceObjectID)
		return nil, fmt.Errorf("reparatronNext: %w", err)
	}
	return packets, nil
}

func (e campaignReparatronSchedule) hit() ([][]byte, error) {
	isDeferred, err := e.runtime.pursuit.deferCommit(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID, e.hit,
	)
	if err != nil {
		return nil, fmt.Errorf("reparatronDefer: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.isCampaignNPCSourceGenerationActive(e.generation, e.sourceObjectID, e.actionGeneration)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.targetObjectID)
	if !isTargetFound || !target.IsDefeated {
		e.runtime.registry.mutex.Unlock()
		return e.next()
	}
	err = e.runtime.death.resetRepairTimer(peerSession.zone, e.targetObjectID)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("repairCorpseTimer: %w", err)
	}
	isReady, err := peerSession.zone.NPCs().ApplyRepairStack(e.targetObjectID, e.minionThreshold, e.otherThreshold)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("repairStack: %w", err)
	}
	if !isReady {
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		return e.approach(e.timestamp)
	}
	revived, isRevived, err := peerSession.zone.ResurrectNPC(
		context.Background(), e.targetObjectID, e.profile.HealFraction,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		e.runtime.releaseAction(e.sessionKey, e.generation, e.sourceObjectID)
		return nil, fmt.Errorf("reparatronResurrect: %w", err)
	}
	if !isRevived {
		e.runtime.registry.mutex.Unlock()
		return e.next()
	}
	revived, isTargetAcquired, err := peerSession.zone.NPCs().AcquireTarget(
		revived.Plan.ObjectID, peerSession.deployedObjectID,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		e.runtime.releaseAction(e.sessionKey, e.generation, e.sourceObjectID)
		return nil, fmt.Errorf("reparatronTarget: %w", err)
	}
	isStarted := false
	// Retained threat entries can make AcquireTarget report a duplicate after
	// resurrection cleared the active target. StartAction restores that target.
	if isTargetAcquired || revived.TargetObjectID == 0 {
		var isFirstAction bool
		revived, isStarted, isFirstAction, err = peerSession.zone.NPCs().StartAction(
			revived.Plan.ObjectID,
			zonenpc.ActionOwner{UserID: peerSession.binding.UserID, PeerGeneration: e.generation},
			peerSession.deployedObjectID,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("repairResume: %w", err)
		}
		// RepairResurrected replaces the ordinary first-aggro animation.
		if isFirstAction && e.runtime.logger != nil {
			e.runtime.logger.Printf("RakNet repaired enemy first action object=%d", revived.Plan.ObjectID)
		}
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	packets, err := npcraknet.ResurrectionState(revived)
	if err != nil {
		return nil, fmt.Errorf("repairState: %w", err)
	}
	animationPacket, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: e.targetObjectID, State: util.HashID("zlm_minn_tc_2_resurrected"),
		Timestamp: e.timestamp, Scale: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("repairReviveAnimation: %w", err)
	}
	packets = append(packets, animationPacket)
	if isStarted {
		step := campaignNPCFirstActionStep{
			runtime: e.runtime, packet: e.packet, sessionKey: e.sessionKey,
			generation: e.generation, objectID: revived.Plan.ObjectID,
			actionGeneration: revived.ActionGeneration,
			timestamp:        e.timestamp + uint64(reparatronReviveDelay/time.Millisecond),
		}
		err = scheduleNPCProducer(e.runtime.registry, e.packet, reparatronReviveDelay, step.produce)
		if err != nil {
			e.runtime.releaseAction(e.sessionKey, e.generation, revived.Plan.ObjectID)
			return nil, fmt.Errorf("repairCombatSchedule: %w", err)
		}
	}
	nextPackets, err := e.next()
	if err != nil {
		return nil, fmt.Errorf("repairComplete: %w", err)
	}
	return append(packets, nextPackets...), nil
}

func reparatronRepairProfile(
	nounName string,
) (zonenpc.ActionProfile, uint32, uint32, bool) {
	profile := zonenpc.ActionProfile{
		Family: zonenpc.ActionResurrect, AbilityName: "Repair",
		AnimationName: "zlm_minn_tc_2_rez", HitDelay: 3233334 * time.Microsecond,
		ReleaseDelay: 3233334 * time.Microsecond, Range: zonenpc.RepairRadius,
	}
	switch strings.ToLower(nounName) {
	case "zelembasicrepair.noun":
		profile.HealFraction = 0.60
		return profile, 2, 6, true
	case "zelembasicrepair_2.noun":
		profile.HealFraction = 0.70
		return profile, 1, 4, true
	case "zelembasicrepair_3.noun":
		profile.HealFraction = 0.80
		return profile, 1, 2, true
	default:
		return zonenpc.ActionProfile{}, 0, 0, false
	}
}

func (r campaignNPCActionRuntime) reparatronRepairCandidates(
	peerSession gameplayPeerSession,
	source zonenpc.Snapshot,
	maximumRange float32,
) []uint32 {
	candidateObjectIDs := make([]uint32, 0)
	for _, candidate := range peerSession.zone.NPCs().DefeatedCandidates(
		source.Plan.Position, maximumRange,
	) {
		if candidate.Faction != source.Faction {
			continue
		}
		isRepairable := peerSession.zone.Death().IsRepairableCorpse(candidate.Plan.ObjectID)
		if !isRepairable {
			// The legacy peer scheduler retains its run outside the shared death
			// session. It must apply the same killing-hit and fade exclusions.
			run := peerSession.enemyDeaths[candidate.Plan.ObjectID]
			isRepairable = run != nil && run.IsRepairableCorpse()
		}
		if !isRepairable {
			continue
		}
		physics, isPhysicsFound := r.program.NounPhysics[candidate.Plan.NounName]
		if !isPhysicsFound || !physics.IsCreatureTypeKnown ||
			physics.CreatureType != reparatronCyberCreatureType {
			continue
		}
		candidateObjectIDs = append(candidateObjectIDs, candidate.Plan.ObjectID)
	}
	return candidateObjectIDs
}

func (r campaignNPCActionRuntime) produceZelemBasicRepair(
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
	profile, minionThreshold, otherThreshold, isProfileFound := reparatronRepairProfile(
		source.Plan.NounName,
	)
	if !isSourceFound || !isProfileFound {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	if peerSession.zone.NPCs().SilenceRemaining(objectID, r.now()) > 0 {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	at := r.now()
	if peerSession.zone.NPCs().RepairBlockRemaining(objectID, at) > 0 {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	if peerSession.zone.NPCRandom() == nil {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	randomRoll := peerSession.zone.NPCRandom().Float64()
	var candidateObjectIDs []uint32
	if randomRoll < zonenpc.RepairChance {
		candidateObjectIDs = r.reparatronRepairCandidates(peerSession, source, profile.Range)
	}
	target, isTargetFound, err := peerSession.zone.NPCs().ShouldRepair(zonenpc.ShouldRepairRequest{
		SourceObjectID: objectID, CandidateObjectIDs: candidateObjectIDs,
		RandomRoll: randomRoll, At: at,
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("repairCondition: %w", err)
	}
	if !isTargetFound {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	r.registry.mutex.Unlock()
	if r.logger != nil {
		r.logger.Printf(
			"RakNet campaign Reparatron repair starting source=%d target=%d noun=%q",
			objectID, target.Plan.ObjectID, target.Plan.NounName,
		)
	}
	alertPacket, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: objectID, State: util.HashID("zlm_minn_tc_2_alert"), Timestamp: timestamp, Scale: 1,
	})
	if err != nil {
		return nil, true, fmt.Errorf("reparatronCast: %w", err)
	}
	schedule := campaignReparatronSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: objectID,
		actionGeneration: source.ActionGeneration,
		targetObjectID:   target.Plan.ObjectID,
		timestamp:        timestamp + uint64(reparatronAlertDelay/time.Millisecond),
		profile:          profile, minionThreshold: minionThreshold,
		otherThreshold: otherThreshold,
	}
	err = scheduleNPCProducer(r.registry, packet, reparatronAlertDelay, schedule.begin)
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, fmt.Errorf("reparatronSchedule: %w", err)
	}
	return [][]byte{alertPacket}, true, nil
}
