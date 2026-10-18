package gameplay

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// Chunk 1003 owns persistent zones independently of the current attack.
type campaignLaserController struct {
	zones  []*campaignLaserZone
	origin game.Vec3
}

func (e *campaignLaserController) finish() ([][]byte, error) {
	packets := make([][]byte, 0)
	for _, laserZone := range e.zones {
		cleanupPackets, err := laserZone.finish()
		if err != nil {
			return nil, fmt.Errorf("laserControllerCleanup: %w", err)
		}
		packets = append(packets, cleanupPackets...)
	}
	return packets, nil
}

func (e campaignConeSchedule) startPersistentLaserZone() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || !member.isCampaignNPCAttackGenerationActiveAt(e.generation,
		e.objectID, e.plan.TargetObjectID, e.actionGeneration, e.runtime.now()) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	controller := member.campaignNPCLaserZones[e.objectID]
	if controller == nil {
		for _, ally := range e.runtime.registry.sessions {
			if ally.zone == member.zone && ally.campaignNPCLaserZones[e.objectID] != nil {
				controller = ally.campaignNPCLaserZones[e.objectID]
				break
			}
		}
	}
	cleanupPackets := make([][]byte, 0)
	if controller != nil && e.plan.SourcePosition.Sub(controller.origin).Length() > 0.5 {
		var cleanupErr error
		cleanupPackets, cleanupErr = controller.finish()
		if cleanupErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("laserMoveCleanup: %w", cleanupErr)
		}
		controller = nil
	}
	if controller == nil {
		controller = &campaignLaserController{origin: e.plan.SourcePosition}
	}
	primary, isPrimaryFound := member.campaignNPCTarget(e.generation, e.plan.TargetObjectID)
	if !isPrimaryFound {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	for _, beam := range controller.zones {
		if beam.isActive && isCampaignConeTarget(beam.origin, beam.endpoint.Sub(beam.origin), primary, 50, 35) {
			// CanCreateLaserZone rejects targets already covered by a zone arc.
			e.runtime.registry.mutex.Unlock()
			return nil, nil
		}
	}
	e.laserZone.origin = e.plan.SourcePosition
	e.laserZone.endpoint = e.laserEndpoints[0]
	packets, err := npcraknet.LaserZoneStart(e.plan, e.laserZone.objectIDs, e.laserEndpoints)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("laserPlacement: %w", err)
	}
	packets = append(cleanupPackets, packets...)
	limit := max(1, int(e.plan.Profile.MaximumTargetCount))
	if len(controller.zones) >= limit {
		cleanupPackets, cleanupErr := controller.zones[0].finish()
		if cleanupErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("laserEvict: %w", cleanupErr)
		}
		controller.zones = controller.zones[1:]
		packets = append(cleanupPackets, packets...)
	}
	e.laserZone.isActive = true
	controller.zones = append(controller.zones, e.laserZone)
	if member.campaignNPCLaserZones == nil {
		member.campaignNPCLaserZones = make(map[uint32]*campaignLaserController)
	}
	member.campaignNPCLaserZones[e.objectID] = controller
	e.runtime.registry.sessions[e.sessionKey] = member
	e.runtime.registry.mutex.Unlock()
	pulse := campaignLaserPulse{cast: e, controller: controller}
	pulse.cast.hitDelay = e.plan.Profile.HitDelay
	pulsePackets, err := pulse.produce()
	if err != nil {
		return nil, fmt.Errorf("laserInitialPulse: %w", err)
	}
	return append(packets, pulsePackets...), nil
}

type campaignLaserPulse struct {
	cast       campaignConeSchedule
	controller *campaignLaserController
	readyAt    time.Time
}

func (e campaignLaserPulse) produce() ([][]byte, error) {
	cast := e.cast
	registry := cast.runtime.registry
	registry.mutex.Lock()
	member, isFound := registry.sessions[cast.sessionKey]
	if !isFound || member.generation != cast.generation || member.zone == nil ||
		member.isZoneTerminal() || !cast.laserZone.isActive {
		registry.mutex.Unlock()
		return nil, nil
	}
	now := cast.runtime.now()
	source, isSourceFound := member.zone.NPCs().LiveNPC(cast.objectID)
	if !isSourceFound || source.Plan.Position.Sub(e.controller.origin).Length() > 0.5 ||
		member.zone.NPCs().StunRemaining(cast.objectID, now) > 0 ||
		member.zone.NPCs().BanishRemaining(cast.objectID, now) > 0 ||
		member.zone.NPCs().SilenceRemaining(cast.objectID, now) > 0 {
		packets, err := e.controller.finish()
		delete(member.campaignNPCLaserZones, cast.objectID)
		registry.sessions[cast.sessionKey] = member
		registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("laserRetire: %w", err)
		}
		return packets, nil
	}
	packets := make([][]byte, 0)
	if !now.Before(e.readyAt) {
		for _, target := range member.zone.LiveNPCTargets() {
			if !isCampaignLaserZoneTarget(cast.plan.SourcePosition, cast.laserEndpoints[0], target) {
				continue
			}
			profile := cast.plan.Profile
			profile.IsRetainedVolumeDamage = true
			plan, err := zonenpc.PlanAreaAttackWithProfile(source, target.ObjectID, target.Position, profile)
			if err != nil {
				registry.mutex.Unlock()
				return nil, fmt.Errorf("laserDamagePlan: %w", err)
			}
			result, err := zonenpc.CommitAttack(member.zone.NPCRandom(), plan,
				source.Plan.NPCProfile.CriticalRating, cast.runtime.program.Critical)
			if err != nil {
				registry.mutex.Unlock()
				return nil, fmt.Errorf("laserDamageRoll: %w", err)
			}
			hits, delta, isApplied, err := cast.runtime.applyEnemyDamage(&member,
				cast.generation, plan, result, cast.timestamp+uint64(cast.hitDelay/time.Millisecond), false, true, false)
			if err != nil {
				registry.mutex.Unlock()
				return nil, fmt.Errorf("laserDamageApply: %w", err)
			}
			packets = append(packets, hits...)
			if isApplied {
				member.queueStatDelta(delta)
				impactPacket, impactErr := npcraknet.AttackImpact(plan)
				if impactErr != nil {
					registry.mutex.Unlock()
					return nil, fmt.Errorf("laserImpact: %w", impactErr)
				}
				packets = append(packets, impactPacket)
			}
		}
		e.readyAt = now.Add(time.Second)
	}
	registry.sessions[cast.sessionKey] = member
	registry.mutex.Unlock()
	delay := min(300*time.Millisecond, e.readyAt.Sub(now))
	if delay <= 0 {
		delay = 300 * time.Millisecond
	}
	e.cast.hitDelay += delay
	err := scheduleNPCProducer(registry, cast.packet, delay, e.produce)
	if err != nil {
		return nil, fmt.Errorf("laserPulseSchedule: %w", err)
	}
	return packets, nil
}

// Match the authored trigger box instead of a zero-width point-to-line test.
func isCampaignLaserZoneTarget(origin, endpoint game.Vec3, target zone.NPCTarget) bool {
	segment := endpoint.Sub(origin)
	lengthSquared := segment.X*segment.X + segment.Y*segment.Y
	if lengthSquared <= 0 {
		return false
	}
	delta := target.Position.Sub(origin)
	projection := (delta.X*segment.X + delta.Y*segment.Y) / lengthSquared
	if projection < 0 || projection > 1 {
		return false
	}
	closest := origin.Add(segment.Scale(projection))
	delta = target.Position.Sub(closest)
	width := float32(0.5) + target.ActorFootprintRadius
	return delta.X*delta.X+delta.Y*delta.Y <= width*width && math.Abs(float64(delta.Z)) <= 0.75
}
