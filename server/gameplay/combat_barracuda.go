package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func planCampaignNPCZelemBlink(
	enemy zonenpc.Snapshot, targetObjectID uint32,
	targetPosition game.Vec3, targetFootprintRadius float32,
) (zonenpc.AttackPlan, error) {
	profile, isFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	if !isFound || profile.Family != zonenpc.ActionZelemRanged ||
		profile.TeleportNormalDistance <= 0 {
		return zonenpc.AttackPlan{}, errors.New("enemy blink unsupported")
	}
	if enemy.IsDefeated || enemy.HitPoint <= 0 || targetObjectID == 0 ||
		enemy.TargetObjectID != targetObjectID {
		return zonenpc.AttackPlan{},
			errors.New("enemy blink target unavailable")
	}
	stopDistance, err := zoneaction.NPCStopDistance(
		profile.Range, enemy.Plan.NPCProfile.FootprintRadius,
		targetFootprintRadius,
	)
	if err != nil {
		return zonenpc.AttackPlan{},
			fmt.Errorf("enemyBlinkRange: %w", err)
	}
	profile.Range = stopDistance
	if zoneability.Distance(enemy.Plan.Position, targetPosition) >=
		profile.Range {
		return zonenpc.AttackPlan{},
			errors.New("enemy blink target out of range")
	}
	return zonenpc.AttackPlan{
		SourceObjectID: enemy.Plan.ObjectID, TargetObjectID: targetObjectID,
		ActionGeneration: enemy.ActionGeneration,
		SourcePosition:   enemy.Plan.Position, TargetPosition: targetPosition,
		Profile: profile,
	}, nil
}

func campaignNPCZelemBlinkDestination(
	plan zonenpc.AttackPlan, targetDirection game.Vec3,
) (game.Vec3, error) {
	if plan.SourceObjectID == 0 || plan.TargetObjectID == 0 ||
		!isFiniteCampaignPopulationPosition(plan.SourcePosition) ||
		!isFiniteCampaignPopulationPosition(plan.TargetPosition) ||
		!isFiniteCampaignPopulationPosition(targetDirection) ||
		plan.Profile.TeleportMinimumDistance <= 0 ||
		plan.Profile.TeleportNormalDistance <
			plan.Profile.TeleportMinimumDistance ||
		plan.Profile.TeleportNormalDistance >
			plan.Profile.TeleportMaximumDistance {
		return game.Vec3{}, errors.New("enemy blink destination invalid")
	}
	// Prefer the target's rear while it moves; otherwise flank past it from
	// the incoming attack direction. Spread a pack across the rear arc.
	if targetDirection.X == 0 && targetDirection.Y == 0 {
		targetDirection = plan.SourcePosition.Sub(plan.TargetPosition)
	}
	angleStep := uint64(plan.SourceObjectID)*137 + plan.ActionGeneration*83
	angle := math.Atan2(float64(-targetDirection.Y), float64(-targetDirection.X)) +
		(float64(angleStep%61)-30)*math.Pi/180
	distance := float64(plan.Profile.TeleportNormalDistance)
	return game.Vec3{
		X: plan.TargetPosition.X + float32(math.Cos(angle)*distance),
		Y: plan.TargetPosition.Y + float32(math.Sin(angle)*distance),
		Z: plan.TargetPosition.Z,
	}, nil
}

type campaignZelemBlinkSchedule struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
	plan       zonenpc.AttackPlan
}

func (e campaignZelemBlinkSchedule) resume(
	timestamp uint64,
) ([][]byte, error) {
	packets, err := e.runtime.produceZelemBlink(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("barracudaResume: %w", err)
	}
	return packets, nil
}

func (e campaignZelemBlinkSchedule) shot() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && current.isCampaignNPCSourceGenerationActive(
		e.generation, e.objectID, e.plan.ActionGeneration,
	)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	timestamp := e.timestamp + uint64(e.plan.Profile.HitDelay/time.Millisecond)
	packets, err := e.runtime.produceZelemShot(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
	if err != nil {
		return e.fail("barracudaShot", err)
	}
	return packets, nil
}

func (e campaignZelemBlinkSchedule) fail(step string, err error) ([][]byte, error) {
	e.runtime.releaseActionGeneration(e.sessionKey, e.generation,
		e.objectID, e.plan.ActionGeneration)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e campaignZelemBlinkSchedule) hit() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && current.isCampaignNPCSourceGenerationActive(
		e.generation, e.objectID, e.plan.ActionGeneration,
	)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if !current.isCampaignNPCActionActiveAt(e.generation, e.objectID, e.runtime.now()) {
		e.runtime.registry.mutex.Unlock()
		return e.shot()
	}
	var err error
	current, err = e.runtime.pursuit.advanceTargetPoseLocked(current, e.plan.TargetObjectID)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("barracudaTargetPose", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = current
	currentNPC, isNPCFound := current.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := current.campaignNPCTarget(
		e.generation, currentNPC.TargetObjectID,
	)
	if !isNPCFound || !isTargetFound {
		e.runtime.registry.mutex.Unlock()
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, nil
	}
	plan, err := planCampaignNPCZelemBlink(
		currentNPC, target.ObjectID, target.Position, target.FootprintRadius,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.shot()
	}
	desired, err := campaignNPCZelemBlinkDestination(plan, target.LinearVelocity)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("barracudaDesired", err)
	}
	destination, isDestinationFound, err := zonenavigation.ReachableTeleportDestination(
		current.zone.Navigation(), target.Position, desired,
		currentNPC.Plan.NPCProfile.FootprintRadius,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("barracudaLanding", err)
	}
	distance := zoneability.Distance(destination, target.Position)
	if !isDestinationFound || distance < plan.Profile.TeleportMinimumDistance ||
		distance > plan.Profile.TeleportMaximumDistance {
		destination, isDestinationFound, err = zonenavigation.RandomTeleportDestination(
			current.zone.Navigation(), current.zone.NPCRandom(),
			zonenavigation.RandomTeleportRequest{
				SourcePosition:  target.Position,
				FootprintRadius: currentNPC.Plan.NPCProfile.FootprintRadius,
				MinimumDistance: plan.Profile.TeleportMinimumDistance,
				NormalDistance:  plan.Profile.TeleportNormalDistance,
				MaximumDistance: plan.Profile.TeleportMaximumDistance,
			},
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return e.fail("barracudaFallback", err)
		}
	}
	if !isDestinationFound {
		e.runtime.registry.mutex.Unlock()
		e.runtime.logger.Printf("RakNet Barracuda blink skipped source=%d target=%d reason=no reachable landing; resuming ranged attack", e.objectID, target.ObjectID)
		return e.shot()
	}
	err = current.zone.NPCs().SetPosition(e.objectID, destination)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("barracudaPosition", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = current
	e.runtime.registry.mutex.Unlock()
	e.runtime.logger.Printf("RakNet Barracuda blink completed source=%d target=%d destination=%.3f,%.3f,%.3f; resuming ranged attack", e.objectID, target.ObjectID, destination.X, destination.Y, destination.Z)
	timestamp := e.timestamp + uint64(plan.Profile.HitDelay/time.Millisecond)
	packets, err := npcraknet.Blink(plan, destination, timestamp)
	if err != nil {
		return e.fail("barracudaTeleport", err)
	}
	shotPackets, err := e.shot()
	if err != nil {
		return nil, fmt.Errorf("enemyZelemShot: %w", err)
	}
	return append(packets, shotPackets...), nil
}

func (e campaignNPCActionRuntime) produceZelemBlink(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	resume := campaignZelemBlinkSchedule{
		runtime: e, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
	}
	isDeferred, err := e.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp,
		resume.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyZelemBlinkStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	e.registry.mutex.RLock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound &&
		peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		e.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	e.registry.mutex.RUnlock()
	if !isEnemyFound || enemy.IsDefeated || !isTargetFound {
		e.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	plan, err := planCampaignNPCZelemBlink(
		enemy, target.ObjectID, target.Position, target.FootprintRadius,
	)
	if err != nil {
		packets, shotErr := e.produceZelemShot(
			packet, sessionKey, generation, objectID, timestamp,
		)
		if shotErr != nil {
			e.releaseActionGeneration(sessionKey, generation, objectID, enemy.ActionGeneration)
			return nil, fmt.Errorf("barracudaFallbackShot: %w", shotErr)
		}
		return packets, nil
	}
	startPackets, err := e.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		e.releaseActionGeneration(sessionKey, generation, objectID, plan.ActionGeneration)
		return nil, fmt.Errorf("enemyZelemBlinkStart: %w", err)
	}
	schedule := campaignZelemBlinkSchedule{
		runtime: e, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		plan: plan,
	}
	hitProducer := raknet.ScheduledPacketProducer{
		Delay: plan.Profile.HitDelay, Produce: schedule.hit,
	}
	// The hit hands ownership to the ranged attack chain. A second timer here
	// races that chain and can replace every shot with another blink.
	cancel, scheduleErr := scheduleNPCProducers(e.registry, packet,
		[]raknet.ScheduledPacketProducer{hitProducer},
	)
	if scheduleErr != nil {
		return schedule.fail("barracudaSchedule", scheduleErr)
	}
	if cancel == nil {
		return schedule.fail("barracudaCancel", errors.New("blink cancellation unavailable"))
	}
	return startPackets, nil
}
