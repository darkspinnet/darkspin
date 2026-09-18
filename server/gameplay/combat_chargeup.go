package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignNPCChargeupState struct {
	isCharged          bool
	nextBuildTimestamp uint64
	revision           uint64
}

type campaignNPCChargeupSchedule struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
	revision   uint64
	profile    zonenpc.ActionProfile
}

func campaignNPCChargeupCooldown(
	cooldown time.Duration, variance time.Duration, draw float64,
) (time.Duration, error) {
	if cooldown <= 0 || variance < 0 || math.IsNaN(draw) || math.IsInf(draw, 0) ||
		draw < 0 || draw >= 1 {
		return 0, errors.New("enemy chargeup cooldown invalid")
	}
	adjustment := time.Duration((draw*2 - 1) * float64(variance))
	result := cooldown + adjustment
	if result <= 0 {
		return 0, errors.New("enemy chargeup cooldown nonpositive")
	}
	return result, nil
}

func (e campaignNPCChargeupSchedule) hit() ([][]byte, error) {
	isDeferred, err := e.runtime.pursuit.deferCommit(
		e.packet, e.sessionKey, e.generation, e.objectID, e.hit,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyChargeupDefer: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	packet, err := npcraknet.ChargeupEffect(
		e.objectID, e.profile.TargetEffectName, false,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyChargeupEffect: %w", err)
	}
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	state := peerSession.campaignNPCChargeups[e.objectID]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		e.generation, e.objectID,
	) && state.revision == e.revision && !state.isCharged
	if isCurrent {
		state.isCharged = true
		peerSession.campaignNPCChargeups[e.objectID] = state
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{packet}, nil
}

func (e campaignNPCChargeupSchedule) next() ([][]byte, error) {
	timestamp := e.timestamp + uint64(e.profile.ReleaseDelay/time.Millisecond)
	packets, err := e.runtime.produceEnemyMelee(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
	if err != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, fmt.Errorf("enemyChargeupNext: %w", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) produceZelemChargeupBuild(
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
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	profile, isProfileFound := zonenpc.ZelemChargeupBuildProfile(enemy.Plan.NounName)
	if !isEnemyFound || !isProfileFound || peerSession.zone.NPCRandom() == nil {
		r.registry.mutex.Unlock()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	if peerSession.zone.NPCs().SilenceRemaining(objectID, r.now()) > 0 {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.campaignNPCChargeups == nil {
		peerSession.campaignNPCChargeups = make(map[uint32]campaignNPCChargeupState)
	}
	previous := peerSession.campaignNPCChargeups[objectID]
	if previous.isCharged || previous.nextBuildTimestamp > timestamp {
		r.registry.mutex.Unlock()
		return r.produceEnemyMelee(packet, sessionKey, generation, objectID, timestamp)
	}
	cooldown, err := campaignNPCChargeupCooldown(
		profile.Cooldown, 500*time.Millisecond,
		peerSession.zone.NPCRandom().Float64(),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyChargeupCooldown: %w", err)
	}
	startPacket, err := npcraknet.AnimationState(
		objectID, profile.AnimationName, timestamp,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyChargeupStart: %w", err)
	}
	state := previous
	state.nextBuildTimestamp = timestamp + uint64(cooldown/time.Millisecond)
	state.revision++
	peerSession.campaignNPCChargeups[objectID] = state
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	schedule := campaignNPCChargeupSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey, generation: generation,
		objectID: objectID, timestamp: timestamp, revision: state.revision,
		profile: profile,
	}
	_, scheduleErr := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{
		{Delay: profile.HitDelay, Produce: schedule.hit},
		{Delay: profile.ReleaseDelay, Produce: schedule.next},
	})
	if scheduleErr != nil {
		r.registry.mutex.Lock()
		latest, isLatestFound := r.registry.sessions[sessionKey]
		if isLatestFound && latest.generation == generation &&
			latest.campaignNPCChargeups[objectID].revision == state.revision {
			latest.campaignNPCChargeups[objectID] = previous
			r.registry.sessions[sessionKey] = latest
		}
		r.registry.mutex.Unlock()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemyChargeupSchedule: %w", scheduleErr)
	}
	return [][]byte{startPacket}, nil
}
