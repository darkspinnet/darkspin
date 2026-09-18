package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type polarisNextPhase uint8

const (
	polarisNextPush polarisNextPhase = iota + 1
	polarisNextMarkSeeker
	polarisNextGravityOrb
)

type campaignNPCPolarisState struct {
	lastBlinkBand      uint8
	nextBlinkTimestamp uint64
}

type campaignNPCPolarisBlinkSchedule struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
	plan       zonenpc.AttackPlan
	nextPhase  polarisNextPhase
}

func (e campaignNPCPolarisBlinkSchedule) fail(
	step string, err error,
) ([][]byte, error) {
	e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func polarisHealthLossBand(hitPoint float32, maximumHitPoint float32) uint8 {
	if maximumHitPoint <= 0 || hitPoint >= maximumHitPoint {
		return 0
	}
	hitPoint = max(hitPoint, 0)
	band := int(math.Floor(float64(
		(maximumHitPoint-hitPoint)*10/maximumHitPoint + 0.000001,
	)))
	return uint8(min(band, 10))
}

func (r campaignNPCActionRuntime) producePolarisPhaseTransition(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, nextPhase polarisNextPhase,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	npc, isNPCFound := peerSession.zone.NPCs().NPC(objectID)
	if !isNPCFound || npc.IsDefeated {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	profile, isProfileFound := zonenpc.ZelemBossBlinkProfile(npc.Plan.NounName)
	if !isProfileFound {
		r.registry.mutex.Unlock()
		return nil, errors.New("enemy Polaris blink profile unavailable")
	}
	previousState := peerSession.campaignNPCPolarisStates[objectID]
	state := previousState
	band := polarisHealthLossBand(npc.HitPoint, npc.Plan.NPCProfile.HitPoint)
	isBlinkReady := band > state.lastBlinkBand && timestamp >= state.nextBlinkTimestamp
	if !isBlinkReady {
		r.registry.mutex.Unlock()
		return r.producePolarisPhase(
			packet, sessionKey, generation, objectID, timestamp, nextPhase,
		)
	}
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, npc.TargetObjectID,
	)
	if !isTargetFound {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	plan := zonenpc.AttackPlan{
		SourceObjectID: objectID, TargetObjectID: target.ObjectID,
		ActionGeneration: npc.ActionGeneration,
		SourcePosition:   npc.Plan.Position, TargetPosition: target.Position,
		Profile: profile,
	}
	if peerSession.campaignNPCPolarisStates == nil {
		peerSession.campaignNPCPolarisStates = make(map[uint32]campaignNPCPolarisState)
	}
	state.lastBlinkBand = band
	state.nextBlinkTimestamp = timestamp + uint64(profile.Cooldown/time.Millisecond)
	peerSession.campaignNPCPolarisStates[objectID] = state
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	startPackets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		r.restoreCampaignNPCPolarisState(
			sessionKey, generation, objectID, previousState,
		)
		return nil, fmt.Errorf("enemyPolarisBlinkStart: %w", err)
	}
	schedule := campaignNPCPolarisBlinkSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		plan: plan, nextPhase: nextPhase,
	}
	cancel, err := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{
		{Delay: profile.HitDelay, Produce: schedule.hit},
		{Delay: profile.ReleaseDelay, Produce: schedule.next},
	})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		r.restoreCampaignNPCPolarisState(
			sessionKey, generation, objectID, previousState,
		)
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemyPolarisBlinkSchedule: %w", err)
	}
	return startPackets, nil
}

func (r campaignNPCActionRuntime) restoreCampaignNPCPolarisState(
	sessionKey string, generation uint64, objectID uint32,
	state campaignNPCPolarisState,
) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if isFound && peerSession.generation == generation {
		if peerSession.campaignNPCPolarisStates == nil {
			peerSession.campaignNPCPolarisStates = make(map[uint32]campaignNPCPolarisState)
		}
		peerSession.campaignNPCPolarisStates[objectID] = state
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
}

func (e campaignNPCPolarisBlinkSchedule) hit() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		e.generation, e.objectID,
	)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	npc, isNPCFound := peerSession.zone.NPCs().NPC(e.objectID)
	if !isNPCFound || npc.IsDefeated || peerSession.zone.NPCRandom() == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	destination, isDestinationFound, err := zonenavigation.RandomTeleportDestination(
		peerSession.zone.Navigation(), peerSession.zone.NPCRandom(),
		zonenavigation.RandomTeleportRequest{
			SourcePosition:  npc.Plan.Position,
			FootprintRadius: npc.Plan.NPCProfile.FootprintRadius,
			MinimumDistance: e.plan.Profile.TeleportMinimumDistance,
			NormalDistance:  e.plan.Profile.TeleportNormalDistance,
			MaximumDistance: e.plan.Profile.TeleportMaximumDistance,
		},
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("enemyPolarisBlinkDestination", err)
	}
	if !isDestinationFound {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	err = peerSession.zone.NPCs().SetPosition(e.objectID, destination)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("enemyPolarisBlinkPosition", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	return npcraknet.Blink(
		e.plan, destination,
		e.timestamp+uint64(e.plan.Profile.HitDelay/time.Millisecond),
	)
}

func (e campaignNPCPolarisBlinkSchedule) next() ([][]byte, error) {
	timestamp := e.timestamp + uint64(e.plan.Profile.ReleaseDelay/time.Millisecond)
	packets, err := e.runtime.producePolarisPhase(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp, e.nextPhase,
	)
	if err != nil {
		return e.fail("enemyPolarisBlinkNext", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) producePolarisPhase(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, nextPhase polarisNextPhase,
) ([][]byte, error) {
	switch nextPhase {
	case polarisNextPush:
		return r.produceEnemyCone(packet, sessionKey, generation, objectID, timestamp)
	case polarisNextMarkSeeker:
		return r.producePolarisMarkSeeker(
			packet, sessionKey, generation, objectID, timestamp,
		)
	case polarisNextGravityOrb:
		return r.producePolarisGravityOrb(
			packet, sessionKey, generation, objectID, timestamp,
		)
	default:
		return nil, errors.New("enemy Polaris phase unavailable")
	}
}

func (r campaignNPCActionRuntime) producePolarisMarkSeeker(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	npc, isNPCFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, npc.TargetObjectID,
	)
	r.registry.mutex.RUnlock()
	if !isNPCFound || npc.IsDefeated || !isTargetFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	profile, isProfileFound := zonenpc.ZelemMarkSeekerProfile(npc.Plan.NounName)
	if !isProfileFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, errors.New("enemy Polaris mark seeker profile unavailable")
	}
	plan, err := zonenpc.PlanAttackWithProfile(
		npc, target.ObjectID, target.Position, profile, target.FootprintRadius,
	)
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	return r.produceZelemShotWithVolley(
		packet, sessionKey, generation, objectID, timestamp,
		campaignNPCProjectileVolley{
			isActive: true, startTimestamp: timestamp,
			retainedTargetPosition: plan.TargetPosition, plan: plan,
		},
	)
}
