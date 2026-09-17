package gameplay

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignResurrectionSchedule struct {
	runtime        campaignNPCActionRuntime
	packet         raknet.Packet
	sessionKey     string
	generation     uint64
	sourceObjectID uint32
	targetObjectID uint32
	timestamp      uint64
	profile        zonenpc.ActionProfile
}

type campaignRezzerFleeStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
}

func (e campaignRezzerFleeStep) produce(
	timestamp uint64,
) ([][]byte, error) {
	profile, isFound := e.runtime.rezzerGhostlyBoltProfile(
		e.sessionKey, e.generation, e.objectID,
	)
	if !isFound {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, nil
	}
	return e.runtime.produceZelemShotWithProfile(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp, profile,
	)
}

func (r campaignNPCActionRuntime) rezzerGhostlyBoltProfile(
	sessionKey string, generation uint64, objectID uint32,
) (zonenpc.ActionProfile, bool) {
	r.registry.mutex.RLock()
	defer r.registry.mutex.RUnlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || !peerSession.isCampaignNPCSourceActive(generation, objectID) {
		return zonenpc.ActionProfile{}, false
	}
	source, isFound := peerSession.zone.NPCs().NPC(objectID)
	if !isFound {
		return zonenpc.ActionProfile{}, false
	}
	return zonenpc.RezzerGhostlyBoltProfile(source.Plan.NounName)
}

func (r campaignNPCActionRuntime) produceRezzerFallback(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(objectID)
	profile, isProfileFound := zonenpc.RezzerGhostlyBoltProfile(source.Plan.NounName)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, source.TargetObjectID,
	)
	if !isSourceFound || !isProfileFound || !isTargetFound ||
		peerSession.zone.NPCRandom() == nil {
		r.registry.mutex.Unlock()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	if zonegeometry.Distance(source.Plan.Position, target.Position) > 10 {
		r.registry.mutex.Unlock()
		return r.produceZelemShotWithProfile(
			packet, sessionKey, generation, objectID, timestamp, profile,
		)
	}
	destination, _, err := campaignChronoStrikerFleeDestination(
		source.Plan.Position, target.Position,
		peerSession.zone.NPCRandom().Float64,
		func(candidate game.Vec3) (game.Vec3, bool, error) {
			return zoneaction.NPCDirectMovementDestination(
				peerSession.zone.Navigation(), source.Plan.Position, candidate,
				source.Plan.NPCProfile.FootprintRadius,
			)
		},
	)
	r.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("enemyRezzerFleeDestination: %w", err)
	}
	fleeProfile := zonenpc.ActionProfile{
		Family: zonenpc.ActionProjectile, AbilityName: "GhostlyBoltFlee",
		MovementSpeed: profile.MovementSpeed,
		Range:         source.Plan.NPCProfile.FootprintRadius,
	}
	action := zonenpc.FirstActionPlan{
		ObjectID: objectID, TargetObjectID: target.ObjectID,
		SourcePosition: source.Plan.Position, TargetPosition: destination,
		Profile: fleeProfile, IsPursuitNeeded: true,
	}
	pursuitPackets, err := npcraknet.Pursuit(action)
	if err != nil {
		return nil, fmt.Errorf("enemyRezzerFleeMarshal: %w", err)
	}
	step := campaignRezzerFleeStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
	}
	err = r.pursuit.schedule(
		packet, sessionKey, generation, objectID, timestamp,
		destination, fleeProfile, step.produce,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyRezzerFleeSchedule: %w", err)
	}
	return pursuitPackets, nil
}

func (e campaignResurrectionSchedule) fail(
	step string, err error,
) ([][]byte, error) {
	e.runtime.releaseAction(e.sessionKey, e.generation, e.sourceObjectID)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e campaignResurrectionSchedule) hit() ([][]byte, error) {
	isDeferred, err := e.runtime.pursuit.deferCommit(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID, e.hit,
	)
	if err != nil {
		return e.fail("enemyResurrectDefer", err)
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
	revived, isRevived, err := peerSession.zone.ResurrectNPC(
		context.Background(), e.targetObjectID, e.profile.HealFraction,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("enemyResurrectApply", err)
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
		return e.fail("enemyResurrectTarget", err)
	}
	isStarted := false
	revived, isStarted, _, err = peerSession.zone.NPCs().StartAction(
		revived.Plan.ObjectID,
		zonenpc.ActionOwner{
			UserID:         peerSession.binding.UserID,
			PeerGeneration: e.generation,
		},
		peerSession.deployedObjectID,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("enemyResurrectAction", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	packets, err := npcraknet.ResurrectionHit(
		e.sourceObjectID, revived, e.profile,
	)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet resurrection presentation omitted source=%d target=%d: %v",
			e.sourceObjectID, revived.Plan.ObjectID, err,
		)
		packets = nil
	}
	if isStarted && e.packet.ScheduleFunc != nil {
		step := campaignNPCFirstActionStep{
			runtime: e.runtime, packet: e.packet, sessionKey: e.sessionKey,
			generation: e.generation, objectID: revived.Plan.ObjectID,
			actionGeneration: revived.ActionGeneration,
			timestamp:        e.timestamp + uint64(e.profile.HitDelay/time.Millisecond),
		}
		scheduleErr := e.packet.ScheduleFunc(0, step.produce)
		if scheduleErr != nil {
			e.runtime.releaseAction(
				e.sessionKey, e.generation, revived.Plan.ObjectID,
			)
			e.runtime.logger.Printf(
				"RakNet resurrected enemy action not scheduled object=%d: %v",
				revived.Plan.ObjectID, scheduleErr,
			)
		}
	}
	return packets, nil
}

func (e campaignResurrectionSchedule) next() ([][]byte, error) {
	nextDelay := max(e.profile.HitDelay, e.profile.ReleaseDelay)
	timestamp := e.timestamp + uint64(nextDelay/time.Millisecond)
	packets, err := e.runtime.produceRezzerFallback(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID, timestamp,
	)
	if err != nil {
		return e.fail("enemyResurrectFallback", err)
	}
	return packets, nil
}

func (e campaignResurrectionSchedule) retry() ([][]byte, error) {
	packets, err := e.runtime.produceResurrection(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID, e.timestamp,
	)
	if err != nil {
		return e.fail("enemyResurrectRetry", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) produceResurrection(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(objectID)
	profile, isProfileFound := zonenpc.ActionProfileForPlan(source.Plan)
	if !isSourceFound || !isProfileFound || profile.Family != zonenpc.ActionResurrect {
		r.registry.mutex.Unlock()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	silenceRemaining := peerSession.zone.NPCs().SilenceRemaining(objectID, r.now())
	if silenceRemaining > 0 {
		r.registry.mutex.Unlock()
		schedule := campaignResurrectionSchedule{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, sourceObjectID: objectID,
			timestamp: timestamp, profile: profile,
		}
		cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
			Delay: silenceRemaining, Produce: schedule.retry,
		}})
		if err == nil && cancel == nil {
			err = errors.New("nil cancellation")
		}
		if err != nil {
			r.releaseAction(sessionKey, generation, objectID)
			return nil, fmt.Errorf("enemyResurrectSilenceSchedule: %w", err)
		}
		return nil, nil
	}
	if peerSession.campaignNPCResurrectionReadiness == nil {
		peerSession.campaignNPCResurrectionReadiness = make(map[uint32]uint64)
	}
	readyTimestamp := peerSession.campaignNPCResurrectionReadiness[objectID]
	if readyTimestamp > timestamp {
		r.registry.mutex.Unlock()
		return r.produceRezzerFallback(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	candidate := peerSession.zone.NPCs().DefeatedCandidates(
		source.Plan.Position, profile.Range,
	)
	eligible := candidate[:0]
	for _, defeated := range candidate {
		if strings.EqualFold(
			defeated.Plan.MarkerSetName, source.Plan.MarkerSetName,
		) {
			eligible = append(eligible, defeated)
		}
	}
	candidate = eligible
	if len(candidate) != 0 {
		peerSession.campaignNPCResurrectionReadiness[objectID] =
			timestamp + uint64(profile.Cooldown/time.Millisecond)
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	schedule := campaignResurrectionSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: objectID,
		timestamp: timestamp, profile: profile,
	}
	nextDelay := max(profile.HitDelay, profile.ReleaseDelay)
	producer := []raknet.ScheduledPacketProducer{{
		Delay: nextDelay, Produce: schedule.next,
	}}
	if len(candidate) == 0 {
		return r.produceRezzerFallback(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	schedule.targetObjectID = candidate[0].Plan.ObjectID
	castPackets, err := npcraknet.ResurrectionCast(
		objectID, schedule.targetObjectID, profile, timestamp,
	)
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemyResurrectCast: %w", err)
	}
	producer = append([]raknet.ScheduledPacketProducer{{
		Delay: profile.HitDelay, Produce: schedule.hit,
	}}, producer...)
	cancel, err := packet.ScheduleProducers(producer)
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemyResurrectSchedule: %w", err)
	}
	return castPackets, nil
}
