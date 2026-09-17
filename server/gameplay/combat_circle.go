package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const campaignCircleTargetTick = 200 * time.Millisecond

type campaignCircleTargetStep struct {
	runtime      campaignNPCActionRuntime
	packet       raknet.Packet
	sessionKey   string
	generation   uint64
	objectID     uint32
	timestamp    uint64
	endTimestamp uint64
	radius       float32
	isClockwise  bool
}

func campaignCircleTargetPoint(
	source game.Vec3, target game.Vec3, radius float32,
	movementSpeed float32, isClockwise bool,
) (game.Vec3, bool) {
	deltaX := source.X - target.X
	deltaY := source.Y - target.Y
	distance := float32(math.Sqrt(float64(deltaX*deltaX + deltaY*deltaY)))
	if distance <= 0 || radius <= 0 || movementSpeed <= 0 {
		return game.Vec3{}, false
	}
	directionX := deltaX / distance
	directionY := deltaY / distance
	lateralX := directionY
	lateralY := -directionX
	if isClockwise {
		lateralX = -lateralX
		lateralY = -lateralY
	}
	lateralDistance := movementSpeed * 0.25
	if radius < distance {
		lateralDistance = distance / radius * 5
	}
	candidateX := target.X + directionX*radius + lateralX*lateralDistance
	candidateY := target.Y + directionY*radius + lateralY*lateralDistance
	candidateDeltaX := candidateX - target.X
	candidateDeltaY := candidateY - target.Y
	candidateDistance := float32(math.Sqrt(float64(
		candidateDeltaX*candidateDeltaX + candidateDeltaY*candidateDeltaY,
	)))
	if candidateDistance <= 0 {
		return game.Vec3{}, false
	}
	return game.Vec3{
		X: target.X + candidateDeltaX/candidateDistance*radius,
		Y: target.Y + candidateDeltaY/candidateDistance*radius,
		Z: target.Z,
	}, true
}

func (e campaignCircleTargetStep) next() ([][]byte, error) {
	nextTimestamp := e.timestamp + uint64(campaignCircleTargetTick/time.Millisecond)
	if nextTimestamp >= e.endTimestamp {
		return e.runtime.produceEnemyCharge(
			e.packet, e.sessionKey, e.generation, e.objectID, e.endTimestamp,
		)
	}
	e.timestamp = nextTimestamp
	return e.produce()
}

func (e campaignCircleTargetStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		e.generation, e.objectID,
	)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		e.generation, enemy.TargetObjectID,
	)
	profile, isProfileFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	if !isEnemyFound || !isTargetFound || !isProfileFound ||
		profile.AbilityName != "DartingAttack" {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	destination, isDestinationFound := campaignCircleTargetPoint(
		enemy.Plan.Position, target.Position, e.radius,
		profile.MovementSpeed, e.isClockwise,
	)
	if isDestinationFound {
		destination, isDestinationFound, _ = zoneaction.NPCDirectMovementDestination(
			peerSession.zone.Navigation(), enemy.Plan.Position, destination,
			enemy.Plan.NPCProfile.FootprintRadius,
		)
	}
	if !isDestinationFound {
		e.isClockwise = !e.isClockwise
		destination, isDestinationFound = campaignCircleTargetPoint(
			enemy.Plan.Position, target.Position, e.radius,
			profile.MovementSpeed, e.isClockwise,
		)
		if isDestinationFound {
			destination, isDestinationFound, _ =
				zoneaction.NPCDirectMovementDestination(
					peerSession.zone.Navigation(), enemy.Plan.Position, destination,
					enemy.Plan.NPCProfile.FootprintRadius,
				)
		}
	}
	if !isDestinationFound {
		destination = target.Position
	}
	step, err := peerSession.zone.NPCs().AdvancePursuit(
		peerSession.zone.Navigation(), e.objectID, destination, 0.1,
		profile.MovementSpeed, enemy.Plan.NPCProfile.FootprintRadius,
		campaignCircleTargetTick,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyCircleAdvance: %w", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	packets, err := npcraknet.PursuitRedirect(
		e.objectID, step.Position, target.ObjectID, target.Position, 0.1,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyCircleRedirect: %w", err)
	}
	cancel, err := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: campaignCircleTargetTick, Produce: e.next,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return nil, fmt.Errorf("enemyCircleSchedule: %w", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) produceVerdanthBasicSkeetCircle(
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
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	profile, isProfileFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	if !isEnemyFound || !isTargetFound || !isProfileFound ||
		profile.AbilityName != "DartingAttack" || peerSession.zone.NPCRandom() == nil {
		r.registry.mutex.Unlock()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	duration := time.Second + time.Duration(
		peerSession.zone.NPCRandom().Float64()*float64(time.Second),
	)
	radius := 7 + float32(peerSession.zone.NPCRandom().Float64()*2)
	isClockwise := peerSession.zone.NPCRandom().Float64() < 0.5
	if zonegeometry.Distance(enemy.Plan.Position, target.Position) > 1.5*radius {
		facingDot := enemy.Facing.X*(enemy.Plan.Position.Y-target.Position.Y) -
			enemy.Facing.Y*(enemy.Plan.Position.X-target.Position.X)
		isClockwise = facingDot < 0
	}
	r.registry.mutex.Unlock()
	step := campaignCircleTargetStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		endTimestamp: timestamp + uint64(duration/time.Millisecond),
		radius:       radius, isClockwise: isClockwise,
	}
	return step.produce()
}
