package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignScaldronNestleWanderStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
	stepIndex  uint32
	profile    zonenpc.ActionProfile
}

func (e campaignScaldronNestleWanderStep) fail(
	step string, err error,
) ([][]byte, error) {
	e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e campaignScaldronNestleWanderStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCActionActiveAt(
		e.generation, e.objectID, e.runtime.now(),
	)
	if !isCurrent || peerSession.zone.NPCRandom() == nil {
		e.runtime.registry.mutex.Unlock()
		if isFound {
			e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		}
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		e.generation, enemy.TargetObjectID,
	)
	if !isEnemyFound || !isTargetFound {
		e.runtime.registry.mutex.Unlock()
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, nil
	}
	distance := e.profile.MinimumRange +
		float32(peerSession.zone.NPCRandom().Float64())*
			(e.profile.Range-e.profile.MinimumRange)
	destination, isDestinationFound, err :=
		zonenavigation.RandomTeleportDestination(
			peerSession.zone.Navigation(), peerSession.zone.NPCRandom(),
			zonenavigation.RandomTeleportRequest{
				SourcePosition:  enemy.Plan.Position,
				FootprintRadius: max(enemy.Plan.NPCProfile.FootprintRadius, 0.25),
				MinimumDistance: e.profile.MinimumRange,
				NormalDistance:  distance,
				MaximumDistance: e.profile.Range,
			},
		)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("nestleWanderDestination", err)
	}
	if !isDestinationFound {
		destination = enemy.Plan.Position
	}
	step, err := peerSession.zone.NPCs().AdvancePursuit(
		peerSession.zone.Navigation(), e.objectID, destination, 0.1,
		e.profile.MovementSpeed, enemy.Plan.NPCProfile.FootprintRadius,
		e.profile.HitDelay,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("nestleWanderAdvance", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	packets, err := npcraknet.PursuitRedirect(
		e.objectID, step.Position, target.ObjectID, destination, 0.1,
	)
	if err != nil {
		return e.fail("nestleWanderRedirect", err)
	}
	stepCount := uint32(e.profile.ModifierDuration / e.profile.HitDelay)
	if e.stepIndex+1 < stepCount {
		next := e
		next.stepIndex++
		next.timestamp += uint64(e.profile.HitDelay / time.Millisecond)
		_, scheduleErr := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
			Delay: e.profile.HitDelay, Produce: next.produce,
		}})
		if scheduleErr != nil {
			return e.fail("nestleWanderSchedule", scheduleErr)
		}
		return packets, nil
	}
	resume := e
	resume.timestamp += uint64(
		(e.profile.HitDelay + e.profile.ReleaseDelay) / time.Millisecond,
	)
	_, scheduleErr := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay:   e.profile.HitDelay + e.profile.ReleaseDelay,
		Produce: resume.resume,
	}})
	if scheduleErr != nil {
		return e.fail("nestleWanderResumeSchedule", scheduleErr)
	}
	return packets, nil
}

func (e campaignScaldronNestleWanderStep) resume() ([][]byte, error) {
	packets, err := e.runtime.produceEnemyMelee(
		e.packet, e.sessionKey, e.generation, e.objectID, e.timestamp,
	)
	if err != nil {
		return e.fail("nestleWanderResume", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) produceScaldronBasicNestleWander(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	if sessionKey == "" || generation == 0 || objectID == 0 {
		return nil, false, errors.New("Nestle wander request invalid")
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		generation, objectID,
	)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	profile, isProfileFound := zonenpc.ScaldronBasicNestleWanderProfile(
		enemy.Plan.NounName,
	)
	readyTimestamp := peerSession.campaignNPCNestleWanderReadiness[objectID]
	if !isEnemyFound || !isProfileFound || readyTimestamp > timestamp {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	if peerSession.campaignNPCNestleWanderReadiness == nil {
		peerSession.campaignNPCNestleWanderReadiness = make(map[uint32]uint64)
	}
	peerSession.campaignNPCNestleWanderReadiness[objectID] = timestamp +
		uint64(profile.Cooldown/time.Millisecond)
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	step := campaignScaldronNestleWanderStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		profile: profile,
	}
	packets, err := step.produce()
	if err != nil {
		return nil, false, fmt.Errorf("nestleWanderStart: %w", err)
	}
	return packets, true, nil
}
