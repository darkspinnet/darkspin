package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const plasmaWallStep = 100 * time.Millisecond

type plasmaWallState struct {
	runs     map[uint32]*plasmaWallRun
	segments map[uint32]*plasmaWallSegment
}

type plasmaWallRun struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	zone             *zone.Zone
	sessionKey       string
	generation       uint64
	objectID         uint32
	actionGeneration uint64
	timestamp        uint64
	origin           game.Vec3
	destination      game.Vec3
	waypoint         game.Vec3
	lastAt           time.Time
	legStartedAt     time.Time
	lineAt           time.Time
	effectAt         time.Time
	isMoving         bool
	isPaused         bool
	isWaypointKnown  bool
	isStopped        bool
	cancel           raknet.CancelSchedule
	tickDamage       game.DamageRange
}

// Chunk467 owns one ongoing movement action. It does not acquire a melee
// target, pay a melee cooldown, or emit a fabricated attack animation.
func (e campaignNPCActionRuntime) producePlasmaWall(packet raknet.Packet, sessionKey string,
	generation uint64, objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.zone == nil {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	actor, isActorFound := member.zone.NPCs().NPC(objectID)
	profile, isKnown := zonenpc.ActionProfileForPlan(actor.Plan)
	if !isActorFound || !isKnown || profile.AbilityName != "PlasmaWall" {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	if !member.isCampaignNPCSourceGenerationActive(generation, objectID, actor.ActionGeneration) ||
		member.plasmaWall.runs[objectID] != nil {
		e.registry.mutex.Unlock()
		return nil, true, nil
	}
	tickDamage, err := zonenpc.ResolveModifierTickDamage(actor,
		zonenpc.PlasmaWallBurnProfile(zonenpc.EffectiveNPCRank(actor.Plan)))
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("plasmaSnapshot: %w", err)
	}
	run := &plasmaWallRun{runtime: e, packet: packet.Autonomous(), zone: member.zone,
		sessionKey: sessionKey, generation: generation, objectID: objectID,
		actionGeneration: actor.ActionGeneration, timestamp: timestamp, lastAt: e.now(), tickDamage: tickDamage}
	if member.plasmaWall.runs == nil {
		member.plasmaWall.runs = make(map[uint32]*plasmaWallRun)
	}
	member.plasmaWall.runs[objectID] = run
	e.registry.sessions[sessionKey] = member
	e.registry.mutex.Unlock()
	packets, err := run.produce()
	if err != nil {
		return packets, true, fmt.Errorf("plasmaStart: %w", err)
	}
	return packets, true, nil
}

func (e *plasmaWallRun) schedule() error {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	if e.isStopped {
		return nil
	}
	cancel, err := scheduleNPCProducers(e.runtime.registry, e.packet,
		[]raknet.ScheduledPacketProducer{{Delay: plasmaWallStep, Produce: e.produce}})
	if err != nil {
		return fmt.Errorf("plasmaSchedule: %w", err)
	}
	if cancel == nil {
		return fmt.Errorf("plasmaSchedule: unavailable")
	}
	e.cancel = cancel
	return nil
}

func (e *plasmaWallRun) produce() ([][]byte, error) {
	packets, err := e.step()
	if err == nil {
		return packets, nil
	}
	e.runtime.registry.mutex.Lock()
	e.stop()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if isFound && member.plasmaWall.runs[e.objectID] == e {
		delete(member.plasmaWall.runs, e.objectID)
		e.runtime.registry.sessions[e.sessionKey] = member
	}
	e.runtime.registry.mutex.Unlock()
	e.runtime.releaseActionGeneration(e.sessionKey, e.generation, e.objectID, e.actionGeneration)
	return packets, fmt.Errorf("plasmaStep: %w", err)
}

func (e *plasmaWallRun) step() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := !e.isStopped && isFound && member.zone == e.zone &&
		member.plasmaWall.runs[e.objectID] == e &&
		member.isCampaignNPCSourceGenerationActive(e.generation, e.objectID, e.actionGeneration)
	if !isCurrent {
		e.stop()
		if isFound && member.plasmaWall.runs[e.objectID] == e {
			delete(member.plasmaWall.runs, e.objectID)
			e.runtime.registry.sessions[e.sessionKey] = member
		}
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	actor, isActorFound := member.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := member.campaignNPCTarget(e.generation, actor.TargetObjectID)
	if !isActorFound || !isTargetFound || target.HitPoint <= 0 || member.isZoneTerminal() {
		e.stop()
		delete(member.plasmaWall.runs, e.objectID)
		e.runtime.registry.sessions[e.sessionKey] = member
		e.runtime.registry.mutex.Unlock()
		e.runtime.releaseActionGeneration(e.sessionKey, e.generation, e.objectID, e.actionGeneration)
		return nil, nil
	}
	if member.zone.NPCRandom() == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("plasmaRandom: unavailable")
	}
	now := e.runtime.now()
	elapsed := max(time.Duration(0), now.Sub(e.lastAt))
	e.timestamp += uint64(elapsed / time.Millisecond)
	e.lastAt = now
	packets := make([][]byte, 0)
	if e.effectAt.IsZero() || !now.Before(e.effectAt) {
		effect, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{
			ObjectID: e.objectID, Asset: util.HashID("fire_wall_boss_effect.ServerEventDef")})
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("plasmaEffect: %w", err)
		}
		packets = append(packets, effect)
		e.effectAt = now.Add(400 * time.Millisecond)
	}
	isActive := member.isCampaignNPCActionActiveAt(e.generation, e.objectID, now)
	isSilenced := member.zone.NPCs().SilenceRemaining(e.objectID, now) > 0
	if !isActive || isSilenced {
		if e.isMoving && !e.isPaused {
			pausePackets, pauseErr := npcraknet.MovementPause(e.objectID, actor.Plan.Position, actor.Facing)
			if pauseErr != nil {
				e.runtime.registry.mutex.Unlock()
				return packets, fmt.Errorf("plasmaPause: %w", pauseErr)
			}
			packets = append(packets, pausePackets...)
			e.isPaused = true
		}
		e.runtime.registry.mutex.Unlock()
		err := e.schedule()
		if err != nil {
			return packets, fmt.Errorf("plasmaHold: %w", err)
		}
		return packets, nil
	}
	if e.isMoving && e.isPaused {
		resumePackets, resumeErr := npcraknet.PursuitWaypoint(e.objectID, e.destination)
		if resumeErr != nil {
			e.runtime.registry.mutex.Unlock()
			return packets, fmt.Errorf("plasmaResume: %w", resumeErr)
		}
		packets = append(packets, resumePackets...)
		e.isPaused = false
		e.isWaypointKnown = false
	}
	if !e.isMoving {
		movePackets, err := e.beginLegLocked(&member, actor, target.Position, now)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return packets, fmt.Errorf("plasmaLeg: %w", err)
		}
		packets = append(packets, movePackets...)
	}
	profile, isKnown := zonenpc.ActionProfileForPlan(actor.Plan)
	if !isKnown {
		e.runtime.registry.mutex.Unlock()
		return packets, fmt.Errorf("plasmaProfile: unavailable")
	}
	speed := profile.MovementSpeed * member.zone.NPCs().SlowMovementScale(e.objectID, now)
	var step zonenpc.PursuitStep
	var err error
	if elapsed > 0 && speed > 0 {
		step, err = member.zone.NPCs().AdvancePursuit(member.zone.Navigation(), e.objectID,
			e.destination, 0.95, speed, actor.NavigationRadius(), elapsed)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return packets, fmt.Errorf("plasmaAdvance: %w", err)
		}
		actor, isActorFound = member.zone.NPCs().NPC(e.objectID)
		if !isActorFound {
			e.runtime.registry.mutex.Unlock()
			return packets, fmt.Errorf("plasmaActor: unavailable")
		}
		if step.IsWaypointKnown && !step.IsInRange &&
			(!e.isWaypointKnown || zonegeometry.DistanceSquared(e.waypoint, step.Waypoint) > 0.0001) {
			waypointPackets, waypointErr := npcraknet.PursuitWaypoint(e.objectID, step.Waypoint)
			if waypointErr != nil {
				e.runtime.registry.mutex.Unlock()
				return packets, fmt.Errorf("plasmaWaypoint: %w", waypointErr)
			}
			packets = append(packets, waypointPackets...)
			e.waypoint, e.isWaypointKnown = step.Waypoint, true
		}
	}
	plans := []zonenpc.AttackPlan{}
	if !now.Before(e.lineAt) {
		plans = plasmaWallContactPlans(member, e.objectID, 0, e.origin, actor.Plan.Position,
			0, zonenpc.EffectiveNPCRank(actor.Plan))
		e.lineAt = now.Add(time.Second)
	}
	if step.IsInRange || now.Sub(e.legStartedAt) >= 5*time.Second {
		wallPackets, wallErr := e.completeLegLocked(&member, actor)
		if wallErr != nil {
			e.runtime.registry.mutex.Unlock()
			return packets, fmt.Errorf("plasmaComplete: %w", wallErr)
		}
		packets = append(packets, wallPackets...)
	}
	e.runtime.registry.sessions[e.sessionKey] = member
	e.runtime.registry.mutex.Unlock()
	for _, plan := range plans {
		burnPackets, burnErr := e.runtime.applyCampaignNPCDisease(nil, e.packet, e.sessionKey, e.generation,
			plan, e.timestamp, e.tickDamage)
		if burnErr != nil {
			return packets, fmt.Errorf("plasmaLineBurn: %w", burnErr)
		}
		packets = append(packets, burnPackets...)
	}
	err = e.schedule()
	if err != nil {
		return packets, fmt.Errorf("plasmaContinue: %w", err)
	}
	return packets, nil
}

func (e *plasmaWallRun) beginLegLocked(member *gameplayPeerSession, actor zonenpc.Snapshot,
	target game.Vec3, now time.Time,
) ([][]byte, error) {
	destination := zonenpc.PlasmaWallDestination(actor.Plan.Position, target, member.zone.NPCRandom().Float64())
	projected, isProjected, err := zoneaction.NPCProjectPosition(member.zone.Navigation(), destination, actor.NavigationRadius())
	if err != nil {
		return nil, fmt.Errorf("plasmaDestination: %w", err)
	}
	if isProjected {
		destination = projected
	} else {
		// FindBallRollPosition's authored failure path uses the target position.
		destination = target
	}
	e.origin, e.destination = actor.Plan.Position, destination
	e.legStartedAt, e.lineAt = now, now
	e.isMoving, e.isWaypointKnown = true, false
	packets, err := npcraknet.PursuitWaypoint(e.objectID, destination)
	if err != nil {
		return nil, fmt.Errorf("plasmaMove: %w", err)
	}
	return packets, nil
}

func (e *plasmaWallRun) stop() {
	e.isStopped = true
	if e.cancel != nil {
		e.cancel()
	}
}
