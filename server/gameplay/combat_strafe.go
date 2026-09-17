package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const (
	campaignNPCStrafeChance        = 0.5
	campaignNPCStrafeMinimumPeriod = 2 * time.Second
	campaignNPCStrafeTick          = 250 * time.Millisecond
	campaignNPCStrafeMaximumPeriod = 3 * time.Second
	campaignNPCIdleMinimum         = 100 * time.Millisecond
	campaignNPCIdleRange           = 500 * time.Millisecond
)

type campaignNPCStrafeResume func(uint64) ([][]byte, error)

type campaignNPCStrafeMode uint8

const (
	campaignNPCStrafeModeOrdinary campaignNPCStrafeMode = iota
	campaignNPCStrafeModeRetreat
	campaignNPCStrafeModeFollowup
)

type campaignNPCStrafeStep struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	actionGeneration uint64
	objectID         uint32
	targetObjectID   uint32
	timestamp        uint64
	destination      game.Vec3
	stopDistance     float32
	movementSpeed    float32
	elapsed          time.Duration
	maximumPeriod    time.Duration
	mode             campaignNPCStrafeMode
	profile          zonenpc.ActionProfile
	resume           campaignNPCStrafeResume
}

type campaignNPCStrafeResumeStep struct {
	runtime          campaignNPCActionRuntime
	sessionKey       string
	generation       uint64
	actionGeneration uint64
	objectID         uint32
	timestamp        uint64
	resume           campaignNPCStrafeResume
}

func (e campaignNPCStrafeResumeStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceGenerationActive(
		e.generation, e.objectID, e.actionGeneration,
	)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	return produceCampaignNPCStrafeResume(e.resume, e.timestamp)
}

func produceCampaignNPCStrafeResume(
	resume campaignNPCStrafeResume, timestamp uint64,
) ([][]byte, error) {
	packets, err := resume(timestamp)
	if err != nil {
		return nil, fmt.Errorf("enemyStrafeResume: %w", err)
	}
	return packets, nil
}

func isCampaignNPCBoundedStrafeProfile(
	profile zonenpc.ActionProfile, isCaptain bool,
) bool {
	if isCaptain {
		switch profile.Family {
		case zonenpc.ActionZelemRanged, zonenpc.ActionZelemHaster,
			zonenpc.ActionNomadSnipe, zonenpc.ActionProjectile,
			zonenpc.ActionPushPull:
			return true
		default:
			return false
		}
	}
	switch profile.AbilityName {
	case "NocturnaBasicRangedSilence", "NomadSpecialThree", "PoisonSpit",
		"ZelemSpecialTwo_Push", "ZelemSpecialTwo_Pull", "NomadDrag_Meteor":
		return true
	default:
		return false
	}
}

func (r campaignNPCActionRuntime) produceBoundedStrafeOrIdle(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, actionGeneration uint64, timestamp uint64,
	profile zonenpc.ActionProfile,
	mode campaignNPCStrafeMode, resume campaignNPCStrafeResume,
) ([][]byte, error) {
	if resume == nil {
		return nil, errors.New("enemy strafe resume unavailable")
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound &&
		peerSession.isCampaignNPCSourceGenerationActive(
			generation, objectID, actionGeneration,
		) &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	if !isEnemyFound ||
		!isCampaignNPCBoundedStrafeProfile(profile, enemy.Plan.IsCaptain) {
		r.registry.mutex.Unlock()
		return produceCampaignNPCStrafeResume(resume, timestamp)
	}
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	npcRandom := peerSession.zone.NPCRandom()
	if enemy.IsDefeated || enemy.IsTurtleActive ||
		!isTargetFound || npcRandom == nil {
		r.registry.mutex.Unlock()
		return produceCampaignNPCStrafeResume(resume, timestamp)
	}
	isRetreat := mode == campaignNPCStrafeModeRetreat
	isNomadDrag := profile.AbilityName == "NomadDrag_Meteor"
	if mode == campaignNPCStrafeModeOrdinary && !isNomadDrag &&
		npcRandom.Float64() >= campaignNPCStrafeChance {
		idleDelay := campaignNPCIdleMinimum + time.Duration(
			npcRandom.Float64()*float64(campaignNPCIdleRange),
		)
		turnPlan := zonenpc.AttackPlan{
			SourceObjectID: objectID, TargetObjectID: target.ObjectID,
			ActionGeneration: actionGeneration, SourcePosition: enemy.Plan.Position,
			TargetPosition: target.Position,
		}
		isTurnNeeded := enemy.IsIdleTurnNeeded(target.Position) &&
			peerSession.zone.NPCs().StunRemaining(objectID, r.now()) <= 0 &&
			peerSession.zone.NPCs().SleepRemaining(objectID, r.now()) <= 0
		if isTurnNeeded {
			isTurnNeeded = peerSession.zone.NPCs().CommitFacing(turnPlan)
		}
		r.registry.mutex.Unlock()
		step := campaignNPCStrafeResumeStep{
			runtime: r, sessionKey: sessionKey, generation: generation,
			actionGeneration: actionGeneration, objectID: objectID,
			timestamp: timestamp + uint64(idleDelay/time.Millisecond), resume: resume,
		}
		cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
			Delay: idleDelay, Produce: step.produce,
		}})
		if err == nil && cancel == nil {
			err = errors.New("nil cancellation")
		}
		if err != nil {
			return nil, fmt.Errorf("enemyStrafeIdleSchedule: %w", err)
		}
		if isTurnNeeded {
			turnPackets, turnErr := npcraknet.FaceTarget(turnPlan, false)
			if turnErr != nil {
				return nil, fmt.Errorf("enemyIdleTurn: %w", turnErr)
			}
			return turnPackets, nil
		}
		return nil, nil
	}
	deltaX := target.Position.X - enemy.Plan.Position.X
	deltaY := target.Position.Y - enemy.Plan.Position.Y
	length := float32(math.Sqrt(float64(deltaX*deltaX + deltaY*deltaY)))
	if length <= 0.001 {
		r.registry.mutex.Unlock()
		return produceCampaignNPCStrafeResume(resume, timestamp)
	}
	lateralX := -deltaY / length
	lateralY := deltaX / length
	isLeft := npcRandom.Float64() < 0.5
	if isRetreat {
		lateralX = -deltaX / length
		lateralY = -deltaY / length
	} else if isLeft {
		lateralX = -lateralX
		lateralY = -lateralY
	}
	movementSpeed := graspingDeadMovementSpeed(
		peerSession, enemy.Plan.Position, profile.MovementSpeed,
	)
	slowMovementScale := peerSession.zone.NPCs().SlowMovementScale(
		objectID, r.now(),
	)
	movementSpeed = max(movementSpeed*slowMovementScale, float32(0.1))
	if isNomadDrag {
		movementSpeed = max(movementSpeed-0.2, float32(0.1))
	}
	movementPeriod := campaignNPCStrafeMinimumPeriod + time.Duration(
		npcRandom.Float64()*float64(
			campaignNPCStrafeMaximumPeriod-campaignNPCStrafeMinimumPeriod,
		),
	)
	distance := movementSpeed * float32(movementPeriod.Seconds())
	if isNomadDrag {
		distance = 12
		movementPeriod = time.Duration(
			float64(distance/movementSpeed) * float64(time.Second),
		)
	}
	desired := enemy.Plan.Position
	desired.X += lateralX * distance
	desired.Y += lateralY * distance
	destination, isDestinationFound, err := navigationClippedMovementDestination(
		peerSession.zone.Navigation(), enemy.Plan.Position, desired,
		max(enemy.Plan.NPCProfile.FootprintRadius, float32(0.25)),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyStrafeDestination: %w", err)
	}
	if !isDestinationFound {
		r.registry.mutex.Unlock()
		return produceCampaignNPCStrafeResume(resume, timestamp)
	}
	stopDistance := max(
		enemy.Plan.NPCProfile.FootprintRadius*1.5, float32(0.25),
	)
	maximumPeriod := time.Duration(
		float64(zonegeometry.Distance(enemy.Plan.Position, destination)/movementSpeed)*
			float64(time.Second),
	) + campaignNPCStrafeTick
	maximumPeriod = min(maximumPeriod, movementPeriod+campaignNPCStrafeTick)
	isFacingCommitted := peerSession.zone.NPCs().CommitFacing(zonenpc.AttackPlan{
		SourceObjectID: objectID, TargetObjectID: target.ObjectID,
		ActionGeneration: actionGeneration, SourcePosition: enemy.Plan.Position,
		TargetPosition: target.Position,
	})
	r.registry.mutex.Unlock()
	if !isFacingCommitted {
		return nil, nil
	}

	step := campaignNPCStrafeStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, actionGeneration: actionGeneration,
		objectID:       objectID,
		targetObjectID: target.ObjectID, timestamp: timestamp,
		destination: destination, stopDistance: stopDistance,
		movementSpeed: movementSpeed, maximumPeriod: maximumPeriod,
		mode: mode, profile: profile, resume: resume,
	}
	packets, err := npcraknet.BoundedStrafe(
		objectID, enemy.Plan.Position, destination,
		target.ObjectID, target.Position, stopDistance,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyStrafeStart: %w", err)
	}
	err = step.schedule()
	if err != nil {
		return nil, err
	}
	return packets, nil
}

func (e campaignNPCStrafeStep) schedule() error {
	next := e
	next.elapsed += campaignNPCStrafeTick
	next.timestamp += uint64(campaignNPCStrafeTick / time.Millisecond)
	cancel, err := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: campaignNPCStrafeTick, Produce: next.produce,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return fmt.Errorf("enemyStrafeSchedule: %w", err)
	}
	return nil
}

func (e campaignNPCStrafeStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound &&
		peerSession.isCampaignNPCSourceGenerationActive(
			e.generation, e.objectID, e.actionGeneration,
		) &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		e.generation, e.targetObjectID,
	)
	if !isEnemyFound || enemy.IsDefeated || !isTargetFound {
		e.runtime.registry.mutex.Unlock()
		if isEnemyFound && !enemy.IsDefeated {
			e.runtime.releaseActionGeneration(
				e.sessionKey, e.generation, e.objectID, e.actionGeneration,
			)
		}
		return nil, nil
	}
	if enemy.IsTurtleActive {
		e.runtime.registry.mutex.Unlock()
		return produceCampaignNPCStrafeResume(e.resume, e.timestamp)
	}
	isMovementBlocked := peerSession.zone.NPCs().StunRemaining(
		e.objectID, e.runtime.now(),
	) > 0 || peerSession.zone.NPCs().SleepRemaining(
		e.objectID, e.runtime.now(),
	) > 0 || peerSession.zone.NPCs().RootRemaining(
		e.objectID, e.runtime.now(),
	) > 0
	if isMovementBlocked {
		e.runtime.registry.mutex.Unlock()
		return produceCampaignNPCStrafeResume(e.resume, e.timestamp)
	}
	step, err := peerSession.zone.NPCs().AdvancePursuit(
		peerSession.zone.Navigation(), e.objectID, e.destination,
		e.stopDistance, e.movementSpeed,
		enemy.Plan.NPCProfile.FootprintRadius, campaignNPCStrafeTick,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return produceCampaignNPCStrafeResume(e.resume, e.timestamp)
	}
	isFacingCommitted := peerSession.zone.NPCs().CommitFacing(zonenpc.AttackPlan{
		SourceObjectID: e.objectID, TargetObjectID: target.ObjectID,
		ActionGeneration: e.actionGeneration, SourcePosition: step.Position,
		TargetPosition: target.Position,
	})
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	if !isFacingCommitted {
		return nil, nil
	}

	packets, err := npcraknet.BoundedStrafe(
		e.objectID, step.Position, e.destination,
		target.ObjectID, target.Position, e.stopDistance,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyStrafeProgress: %w", err)
	}
	if step.IsInRange || e.elapsed >= e.maximumPeriod {
		if e.mode == campaignNPCStrafeModeRetreat {
			followupPackets, followupErr := e.runtime.produceBoundedStrafeOrIdle(
				e.packet, e.sessionKey, e.generation, e.objectID,
				e.actionGeneration, e.timestamp, e.profile,
				campaignNPCStrafeModeFollowup, e.resume,
			)
			if followupErr != nil {
				return nil, fmt.Errorf("enemyStrafeFollowup: %w", followupErr)
			}
			return append(packets, followupPackets...), nil
		}
		resumePackets, resumeErr := produceCampaignNPCStrafeResume(
			e.resume, e.timestamp,
		)
		if resumeErr != nil {
			return nil, fmt.Errorf("enemyStrafeFinish: %w", resumeErr)
		}
		return append(packets, resumePackets...), nil
	}
	err = e.schedule()
	if err != nil {
		return nil, err
	}
	return packets, nil
}
