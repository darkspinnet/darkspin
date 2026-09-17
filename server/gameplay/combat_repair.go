package gameplay

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const reparatronFailedRollCooldown = 2 * time.Second
const reparatronCyberCreatureType = uint32(0)

type campaignReparatronSchedule struct {
	runtime         campaignNPCActionRuntime
	packet          raknet.Packet
	sessionKey      string
	generation      uint64
	sourceObjectID  uint32
	targetObjectID  uint32
	timestamp       uint64
	profile         zonenpc.ActionProfile
	minionThreshold uint32
	otherThreshold  uint32
}

func (e campaignReparatronSchedule) next() ([][]byte, error) {
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
		peerSession.isCampaignNPCSourceActive(e.generation, e.sourceObjectID)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.targetObjectID)
	if !isTargetFound || !target.IsDefeated {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.campaignNPCRepairStacks == nil {
		peerSession.campaignNPCRepairStacks = make(map[uint32]uint32)
	}
	stackThreshold := e.otherThreshold
	targetFamily := campaignDifficultyNounFamily(target.Plan.NounName)
	if strings.Contains(targetFamily, "basic") ||
		strings.Contains(targetFamily, "specific") ||
		strings.Contains(targetFamily, "_minn_") {
		stackThreshold = e.minionThreshold
	}
	stackCount := peerSession.campaignNPCRepairStacks[e.targetObjectID] + 1
	peerSession.campaignNPCRepairStacks[e.targetObjectID] = stackCount
	if stackCount < stackThreshold {
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		packet, err := npcraknet.PositionedEffect(
			"cyber_common_repair.ServerEventDef", target.Plan.Position,
		)
		if err != nil {
			return nil, fmt.Errorf("reparatronStackEffect: %w", err)
		}
		return [][]byte{packet}, nil
	}
	delete(peerSession.campaignNPCRepairStacks, e.targetObjectID)
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
		return nil, nil
	}
	_, _, err = peerSession.zone.NPCs().AcquireTarget(
		revived.Plan.ObjectID, peerSession.deployedObjectID,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		e.runtime.releaseAction(e.sessionKey, e.generation, e.sourceObjectID)
		return nil, fmt.Errorf("reparatronTarget: %w", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	packets, err := npcraknet.ResurrectionHit(
		e.sourceObjectID, revived, e.profile,
	)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet campaign Reparatron resurrection presentation omitted source=%d target=%d: %v",
			e.sourceObjectID, e.targetObjectID, err,
		)
		return nil, nil
	}
	return packets, nil
}

func reparatronRepairProfile(
	nounName string,
) (zonenpc.ActionProfile, uint32, uint32, bool) {
	profile := zonenpc.ActionProfile{
		Family: zonenpc.ActionResurrect, AbilityName: "Repair",
		AnimationName: "zlm_minn_tc_2_alert", HitDelay: 4400 * time.Millisecond,
		ReleaseDelay: 4400 * time.Millisecond, Range: 20,
		TargetEffectName: "cyber_common_repair.ServerEventDef",
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

func (r campaignNPCActionRuntime) reparatronRepairCandidate(
	peerSession gameplayPeerSession,
	source zonenpc.Snapshot,
	maximumRange float32,
) (zonenpc.Snapshot, bool) {
	for _, candidate := range peerSession.zone.NPCs().DefeatedCandidates(
		source.Plan.Position, maximumRange,
	) {
		if candidate.Faction != source.Faction {
			continue
		}
		physics, isPhysicsFound := r.program.NounPhysics[candidate.Plan.NounName]
		if !isPhysicsFound || !physics.IsCreatureTypeKnown ||
			physics.CreatureType != reparatronCyberCreatureType {
			continue
		}
		return candidate, true
	}
	return zonenpc.Snapshot{}, false
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
	if timestamp < peerSession.campaignNPCRepairLockouts[objectID] {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	if peerSession.zone.NPCRandom() == nil ||
		peerSession.zone.NPCRandom().Float64() >= 0.5 {
		if peerSession.campaignNPCRepairLockouts == nil {
			peerSession.campaignNPCRepairLockouts = make(map[uint32]uint64)
		}
		peerSession.campaignNPCRepairLockouts[objectID] = timestamp +
			uint64(reparatronFailedRollCooldown/time.Millisecond)
		r.registry.sessions[sessionKey] = peerSession
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	target, isTargetFound := r.reparatronRepairCandidate(
		peerSession, source, profile.Range,
	)
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
	castPackets, err := npcraknet.ResurrectionCast(
		objectID, target.Plan.ObjectID, profile, timestamp,
	)
	if err != nil {
		return nil, true, fmt.Errorf("reparatronCast: %w", err)
	}
	schedule := campaignReparatronSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: objectID,
		targetObjectID: target.Plan.ObjectID,
		timestamp:      timestamp + uint64(profile.HitDelay/time.Millisecond),
		profile:        profile, minionThreshold: minionThreshold,
		otherThreshold: otherThreshold,
	}
	_, err = packet.ScheduleProducers([]raknet.ScheduledPacketProducer{
		{Delay: profile.HitDelay, Produce: schedule.hit},
		{Delay: profile.ReleaseDelay, Produce: schedule.next},
	})
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, fmt.Errorf("reparatronSchedule: %w", err)
	}
	return castPackets, true, nil
}
