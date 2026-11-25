package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignScaldronNestleWanderStep struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	objectID         uint32
	timestamp        uint64
	actionGeneration uint64
	startedAt        time.Time
	advancedAt       time.Time
	destination      game.Vec3
	waypoint         game.Vec3
	isMoving         bool
	profile          zonenpc.ActionProfile
}

func (e campaignScaldronNestleWanderStep) fail(
	step string, err error,
) ([][]byte, error) {
	e.runtime.releaseActionGeneration(e.sessionKey, e.generation, e.objectID, e.actionGeneration)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e campaignScaldronNestleWanderStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.isCampaignNPCActionActiveAt(e.generation, e.objectID, e.runtime.now())
	if !isCurrent || member.zone.NPCRandom() == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	enemy, isEnemyFound := member.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := member.campaignNPCTarget(e.generation, enemy.TargetObjectID)
	if !isEnemyFound || !isTargetFound || enemy.ActionGeneration != e.actionGeneration {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	now := e.runtime.now()
	elapsed := now.Sub(e.advancedAt)
	e.advancedAt = now
	packets := make([][]byte, 0)
	if e.isMoving {
		speed := enemy.Plan.NPCProfile.BaseCombatSpeed
		if speed <= 0 {
			speed = e.profile.MovementSpeed
		}
		speed *= max(float32(0), 1+zonenpc.EffectiveMovementSpeedBuff(enemy.Plan))
		speed *= member.zone.NPCs().SlowMovementScale(e.objectID, now)
		step, err := member.zone.NPCs().AdvancePursuit(member.zone.Navigation(), e.objectID,
			e.destination, 0.1, speed, enemy.NavigationRadius(), elapsed)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return e.fail("nestleWanderAdvance", err)
		}
		enemy.Plan.Position = step.Position
		if step.IsBlocked || step.IsInRange {
			e.isMoving = false
		} else {
			waypoint := e.destination
			if step.IsWaypointKnown {
				waypoint = step.Waypoint
			}
			if waypoint != e.waypoint {
				moves, err := npcraknet.CircleTargetMovement(e.objectID, waypoint, target.ObjectID)
				if err != nil {
					e.runtime.registry.mutex.Unlock()
					return e.fail("nestleWanderWaypoint", err)
				}
				e.waypoint = waypoint
				packets = append(packets, moves...)
			}
		}
	}
	isComplete := !now.Before(e.startedAt.Add(e.profile.ModifierDuration))
	if !e.isMoving && !isComplete {
		distance := e.profile.MinimumRange + float32(member.zone.NPCRandom().Float64())*(e.profile.Range-e.profile.MinimumRange)
		destination, isDestinationFound, err := zonenavigation.RandomTeleportDestination(
			member.zone.Navigation(), member.zone.NPCRandom(), zonenavigation.RandomTeleportRequest{
				SourcePosition: enemy.Plan.Position, FootprintRadius: enemy.NavigationRadius(),
				MinimumDistance: e.profile.MinimumRange, NormalDistance: distance, MaximumDistance: e.profile.Range,
			})
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return e.fail("nestleWanderDestination", err)
		}
		if isDestinationFound {
			waypoint := destination
			if member.zone.Navigation() != nil {
				// Query the first corridor point without advancing the live actor.
				projection, err := zoneaction.AdvancePursuitPathStep(member.zone.Navigation(), enemy.Plan.Position,
					destination, enemy.NavigationRadius(), 0.0001, enemy.Navigation)
				if err != nil {
					e.runtime.registry.mutex.Unlock()
					return e.fail("nestleWanderRoute", err)
				}
				waypoint = projection.Waypoint
			}
			moves, err := npcraknet.CircleTargetMovement(e.objectID, waypoint, target.ObjectID)
			if err != nil {
				e.runtime.registry.mutex.Unlock()
				return e.fail("nestleWanderStart", err)
			}
			e.destination, e.waypoint, e.isMoving = destination, waypoint, true
			packets = append(packets, moves...)
		}
	}
	e.runtime.registry.sessions[e.sessionKey] = member
	e.runtime.registry.mutex.Unlock()
	if isComplete {
		stops, err := npcraknet.StopAtPose(e.objectID, enemy.Plan.Position, enemy.Facing)
		if err != nil {
			return e.fail("nestleWanderStop", err)
		}
		packets = append(packets, stops...)
		e.timestamp += uint64(e.profile.ReleaseDelay / time.Millisecond)
		cancel, scheduleErr := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{
			Delay: e.profile.ReleaseDelay, Produce: e.resume,
		}})
		if scheduleErr == nil && cancel == nil {
			scheduleErr = errors.New("nil cancellation")
		}
		if scheduleErr != nil {
			return e.fail("nestleWanderResumeSchedule", scheduleErr)
		}
		return packets, nil
	}
	e.timestamp += uint64(campaignPursuitFallbackTick / time.Millisecond)
	cancel, scheduleErr := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{
		Delay: campaignPursuitFallbackTick, Produce: e.produce,
	}})
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		return e.fail("nestleWanderSchedule", scheduleErr)
	}
	return packets, nil
}

func (e campaignScaldronNestleWanderStep) resume() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.isCampaignNPCSourceGenerationActive(e.generation, e.objectID, e.actionGeneration)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
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
		profile: profile, actionGeneration: enemy.ActionGeneration, startedAt: r.now(), advancedAt: r.now(),
	}
	packets, err := step.produce()
	if err != nil {
		return nil, false, fmt.Errorf("nestleWanderStart: %w", err)
	}
	return packets, true, nil
}
